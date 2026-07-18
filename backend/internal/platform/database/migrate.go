package database

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"regexp"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Minimal forward-only migration runner (ADR-0011).
//
// Guarantees:
//   - Deterministic order (numeric version prefix).
//   - Each migration applies in its own transaction (PostgreSQL DDL is
//     transactional) — a failure leaves no half-applied migration.
//   - Applied migrations are checksummed; editing history is detected at
//     boot and refuses startup (tamper-evident, matching ADR-0006 posture).
//   - A session-level advisory lock serializes concurrent replicas.

// Migration is one embedded SQL file.
type Migration struct {
	Version  int
	Name     string
	SQL      string
	Checksum string // hex SHA-256 of the file contents
}

var migrationFilePattern = regexp.MustCompile(`^(\d{4})_([a-z0-9_]+)\.sql$`)

// advisoryLockKey is an arbitrary but fixed 64-bit key identifying "schema
// migration" within this database. Chosen once, never reused elsewhere.
const advisoryLockKey int64 = 0x4C45474C53544B01 // "LEGLSTK" + 0x01

// LoadMigrations reads and validates migration files from fsys (typically
// the embedded FS). It fails on gaps in numbering, duplicates, or misnamed
// files — silently skipping a migration is worse than refusing to boot.
func LoadMigrations(fsys fs.FS) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("migrate: read dir: %w", err)
	}

	var migrations []Migration
	seen := map[int]string{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "migrations.go" {
			continue
		}
		m := migrationFilePattern.FindStringSubmatch(name)
		if m == nil {
			return nil, fmt.Errorf("migrate: file %q does not match NNNN_name.sql", name)
		}
		version, err := strconv.Atoi(m[1])
		if err != nil || version == 0 {
			return nil, fmt.Errorf("migrate: file %q has invalid version", name)
		}
		if prev, dup := seen[version]; dup {
			return nil, fmt.Errorf("migrate: duplicate version %04d (%s and %s)", version, prev, name)
		}
		seen[version] = name

		contents, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("migrate: read %q: %w", name, err)
		}
		sum := sha256.Sum256(contents)
		migrations = append(migrations, Migration{
			Version:  version,
			Name:     m[2],
			SQL:      string(contents),
			Checksum: hex.EncodeToString(sum[:]),
		})
	}

	sort.Slice(migrations, func(i, j int) bool { return migrations[i].Version < migrations[j].Version })
	for i, m := range migrations {
		if m.Version != i+1 {
			return nil, fmt.Errorf("migrate: versions must be contiguous from 0001; found %04d at position %d", m.Version, i+1)
		}
	}
	return migrations, nil
}

// Migrate applies all pending migrations, verifying checksums of already
// applied ones first. Safe to run from multiple replicas concurrently.
func Migrate(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS, logger *slog.Logger) error {
	migrations, err := LoadMigrations(fsys)
	if err != nil {
		return err
	}

	// A dedicated connection holds the advisory lock for the whole run;
	// pool queries could execute on different connections and break locking.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("migrate: acquire connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", advisoryLockKey); err != nil {
		return fmt.Errorf("migrate: advisory lock: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", advisoryLockKey)
	}()

	if _, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version     integer PRIMARY KEY,
			name        text        NOT NULL,
			checksum    text        NOT NULL,
			applied_at  timestamptz NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("migrate: ensure schema_migrations: %w", err)
	}

	applied := map[int]string{}
	rows, err := conn.Query(ctx, "SELECT version, checksum FROM schema_migrations ORDER BY version")
	if err != nil {
		return fmt.Errorf("migrate: read applied: %w", err)
	}
	for rows.Next() {
		var version int
		var checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			rows.Close()
			return fmt.Errorf("migrate: scan applied: %w", err)
		}
		applied[version] = checksum
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("migrate: read applied: %w", err)
	}

	for _, m := range migrations {
		if checksum, done := applied[m.Version]; done {
			if checksum != m.Checksum {
				return fmt.Errorf(
					"migrate: checksum mismatch for %04d_%s: applied %s, embedded %s — migration history was modified",
					m.Version, m.Name, checksum, m.Checksum)
			}
			continue
		}
		if err := applyOne(ctx, conn.Conn(), m); err != nil {
			return err
		}
		logger.InfoContext(ctx, "migration applied", "version", m.Version, "name", m.Name)
	}
	return nil
}

func applyOne(ctx context.Context, conn *pgx.Conn, m Migration) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("migrate %04d_%s: begin: %w", m.Version, m.Name, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Fail fast instead of queueing behind live traffic while holding locks.
	if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout = '10s'; SET LOCAL statement_timeout = '120s'"); err != nil {
		return fmt.Errorf("migrate %04d_%s: set timeouts: %w", m.Version, m.Name, err)
	}
	if _, err := tx.Exec(ctx, m.SQL); err != nil {
		return fmt.Errorf("migrate %04d_%s: %w", m.Version, m.Name, err)
	}
	if _, err := tx.Exec(ctx,
		"INSERT INTO schema_migrations (version, name, checksum) VALUES ($1, $2, $3)",
		m.Version, m.Name, m.Checksum); err != nil {
		return fmt.Errorf("migrate %04d_%s: record: %w", m.Version, m.Name, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("migrate %04d_%s: commit: %w", m.Version, m.Name, err)
	}
	return nil
}
