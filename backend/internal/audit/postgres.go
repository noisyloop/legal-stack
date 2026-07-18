package audit

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Recorder appends events to the audit log. It takes the caller's open
// transaction so the audit record and the state change it describes commit
// or roll back atomically — the core guarantee of ADR-0006.
type Recorder struct{}

func NewRecorder() *Recorder { return &Recorder{} }

// Append validates the event, links it to its chain, and inserts it inside
// tx. It sets ID/OccurredAt/OrgSeq/PrevHash/Hash on the event.
func (r *Recorder) Append(ctx context.Context, tx pgx.Tx, e *Event) error {
	if err := e.Validate(); err != nil {
		return err
	}
	if e.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("audit: generate id: %w", err)
		}
		e.ID = id
	}
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now().UTC()
	}
	// PostgreSQL stores microseconds; hash exactly what will round-trip.
	e.OccurredAt = e.OccurredAt.UTC().Truncate(time.Microsecond)

	chainKey := e.ChainKey()

	// Serialize appends per chain with a transaction-scoped advisory lock.
	// Without it, two concurrent transactions could read the same chain head
	// and both claim org_seq N+1; the unique index would abort one, turning
	// legitimate concurrency into user-visible errors. The lock releases
	// automatically at commit/rollback. Class 0x41 ("A") namespaces audit
	// locks away from the migration lock.
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1, $2)",
		int32(0x41554449), chainLockKey(chainKey)); err != nil {
		return fmt.Errorf("audit: chain lock: %w", err)
	}

	// Load the chain head under the lock.
	var prevSeq int64
	prevHash := GenesisHash
	err := tx.QueryRow(ctx, `
		SELECT org_seq, hash FROM audit_log
		WHERE COALESCE(org_id, '00000000-0000-0000-0000-000000000000'::uuid) = $1
		ORDER BY org_seq DESC LIMIT 1`, chainKey).Scan(&prevSeq, &prevHash)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("audit: read chain head: %w", err)
	}

	e.OrgSeq = prevSeq + 1
	e.PrevHash = prevHash
	hash, err := ComputeHash(prevHash, e)
	if err != nil {
		return err
	}
	e.Hash = hash

	var ip *string
	if e.IP != nil {
		s := e.IP.String()
		ip = &s
	}
	metadata, err := metadataJSON(e.Metadata)
	if err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_log (
			id, occurred_at, org_id, org_seq, matter_id,
			actor_type, actor_user_id, action, resource_type, resource_id,
			old_values, new_values, metadata,
			ip, user_agent, request_id, correlation_id,
			prev_hash, hash
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`,
		e.ID, e.OccurredAt, e.OrgID, e.OrgSeq, e.MatterID,
		string(e.ActorType), e.ActorUserID, string(e.Action), e.ResourceType, e.ResourceID,
		e.OldValues, e.NewValues, metadata,
		ip, nullIfEmpty(e.UserAgent), nullIfEmpty(e.RequestID), nullIfEmpty(e.CorrelationID),
		e.PrevHash, e.Hash,
	); err != nil {
		return fmt.Errorf("audit: insert event: %w", err)
	}
	return nil
}

// VerifyOrgChain re-walks an organization's full chain from genesis and
// verifies every link. Run by the background verifier; a failure is a
// security incident, not an ops incident.
func (r *Recorder) VerifyOrgChain(ctx context.Context, pool *pgxpool.Pool, orgID uuid.UUID) error {
	rows, err := pool.Query(ctx, `
		SELECT id, occurred_at, org_id, org_seq, matter_id,
		       actor_type, actor_user_id, action, resource_type, resource_id,
		       old_values, new_values, metadata,
		       host(ip), user_agent, request_id, correlation_id,
		       prev_hash, hash
		FROM audit_log
		WHERE COALESCE(org_id, '00000000-0000-0000-0000-000000000000'::uuid) = $1
		ORDER BY org_seq ASC`, orgID)
	if err != nil {
		return fmt.Errorf("audit: query chain: %w", err)
	}
	defer rows.Close()

	prevHash := GenesisHash
	expectedSeq := int64(1)
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return err
		}
		if err := VerifyChain(prevHash, []*Event{e}); err != nil {
			return err
		}
		if e.OrgSeq != expectedSeq {
			return fmt.Errorf("audit: chain gap: org_seq %d, want %d", e.OrgSeq, expectedSeq)
		}
		prevHash = e.Hash
		expectedSeq++
	}
	return rows.Err()
}

// chainLockKey folds a chain UUID into the 32-bit advisory-lock keyspace.
// Collisions between orgs are harmless: they only cause spurious append
// serialization, never correctness issues.
func chainLockKey(id uuid.UUID) int32 {
	var v uint32
	for i := 0; i < len(id); i += 4 {
		v ^= uint32(id[i])<<24 | uint32(id[i+1])<<16 | uint32(id[i+2])<<8 | uint32(id[i+3])
	}
	return int32(v) // #nosec G115 -- lock keys are arbitrary 32-bit values; wraparound is expected and harmless
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
