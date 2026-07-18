package identity_test

// Integration tests run against a real PostgreSQL (schema via the real
// migrations) because this module's guarantees — unique constraints, FK
// integrity, transactional audit, append-only enforcement — live in the
// database. Set TEST_DATABASE_URL to run; CI provides a service container.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/noisyloop/legal-stack/backend/internal/audit"
	"github.com/noisyloop/legal-stack/backend/internal/identity"
	"github.com/noisyloop/legal-stack/backend/internal/platform/database"
	"github.com/noisyloop/legal-stack/backend/migrations"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := database.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := database.Migrate(ctx, pool, migrations.FS, logger); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return pool
}

func uniqueSlug(prefix string) string {
	return fmt.Sprintf("%s-%s", prefix, strings.ReplaceAll(uuid.NewString()[:13], "-", "x"))
}

func provision(t *testing.T, svc *identity.Service, slug string) *identity.ProvisionOrganizationResult {
	t.Helper()
	res, err := svc.ProvisionOrganization(context.Background(), identity.ProvisionOrganizationCommand{
		Slug: slug, Name: "Test Firm LLP", Kind: identity.OrgKindLawFirm,
		OwnerEmail: slug + "@example.com", OwnerName: "Owner Person",
		RequestID: "it-req-1",
	})
	if err != nil {
		t.Fatalf("ProvisionOrganization: %v", err)
	}
	return res
}

func TestProvisionOrganizationCommitsStateAndAuditAtomically(t *testing.T) {
	pool := testPool(t)
	repos := identity.NewPostgresRepositories(pool)
	svc := identity.NewService(pool, repos, audit.NewRecorder())
	ctx := context.Background()

	slug := uniqueSlug("acme")
	res := provision(t, svc, slug)

	// State landed.
	org, err := repos.GetBySlug(ctx, slug)
	if err != nil {
		t.Fatalf("GetBySlug: %v", err)
	}
	if org.ID != res.Organization.ID {
		t.Error("stored org ID mismatch")
	}
	owner, err := repos.Users().GetByEmail(ctx, slug+"@example.com")
	if err != nil {
		t.Fatalf("GetByEmail: %v", err)
	}
	memberships, err := repos.Memberships().ListByUser(ctx, owner.ID)
	if err != nil || len(memberships) != 1 {
		t.Fatalf("memberships = %v, %v; want exactly one", memberships, err)
	}
	if memberships[0].Role != identity.RoleOrgOwner {
		t.Errorf("role = %q, want org_owner", memberships[0].Role)
	}

	// Audit landed in the same commit: 3 events, valid chain from genesis.
	var count int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM audit_log WHERE org_id = $1", org.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("audit events = %d, want 3", count)
	}
	if err := audit.NewRecorder().VerifyOrgChain(ctx, pool, org.ID); err != nil {
		t.Fatalf("chain verification failed on fresh data: %v", err)
	}
}

func TestProvisionRollsBackAtomically(t *testing.T) {
	pool := testPool(t)
	repos := identity.NewPostgresRepositories(pool)
	svc := identity.NewService(pool, repos, audit.NewRecorder())
	ctx := context.Background()

	slug := uniqueSlug("dup")
	provision(t, svc, slug)

	// Same slug again: the org INSERT fails, and nothing else may survive —
	// in particular no orphan user and no audit events from the dead tx.
	email := "second-" + slug + "@example.com"
	_, err := svc.ProvisionOrganization(ctx, identity.ProvisionOrganizationCommand{
		Slug: slug, Name: "Dup Firm", Kind: identity.OrgKindLawFirm,
		OwnerEmail: email, OwnerName: "Nobody",
	})
	if !errors.Is(err, identity.ErrAlreadyExists) {
		t.Fatalf("error = %v, want ErrAlreadyExists", err)
	}
	if _, err := repos.Users().GetByEmail(ctx, email); !errors.Is(err, identity.ErrNotFound) {
		t.Errorf("orphan user survived a rolled-back provision: %v", err)
	}
}

func TestAuditLogIsAppendOnly(t *testing.T) {
	pool := testPool(t)
	repos := identity.NewPostgresRepositories(pool)
	svc := identity.NewService(pool, repos, audit.NewRecorder())
	ctx := context.Background()

	res := provision(t, svc, uniqueSlug("immut"))
	orgID := res.Organization.ID

	if _, err := pool.Exec(ctx,
		"UPDATE audit_log SET action = 'tampered' WHERE org_id = $1", orgID); err == nil {
		t.Fatal("UPDATE on audit_log must be rejected by trigger")
	} else if !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("unexpected rejection error: %v", err)
	}

	if _, err := pool.Exec(ctx,
		"DELETE FROM audit_log WHERE org_id = $1", orgID); err == nil {
		t.Fatal("DELETE on audit_log must be rejected by trigger")
	}
}

func TestChainVerificationSpansMultipleProvisions(t *testing.T) {
	pool := testPool(t)
	repos := identity.NewPostgresRepositories(pool)
	recorder := audit.NewRecorder()
	svc := identity.NewService(pool, repos, recorder)
	ctx := context.Background()

	res := provision(t, svc, uniqueSlug("chain"))
	orgID := res.Organization.ID

	// Append more events to the same org chain across transactions.
	for i := 0; i < 4; i++ {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		e := &audit.Event{
			OrgID: &orgID, ActorType: audit.ActorSystem,
			Action: "organization.touch", ResourceType: "organization", ResourceID: &orgID,
		}
		if err := recorder.Append(ctx, tx, e); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}

	if err := recorder.VerifyOrgChain(ctx, pool, orgID); err != nil {
		t.Fatalf("chain broken across transactions: %v", err)
	}

	var maxSeq int64
	if err := pool.QueryRow(ctx,
		"SELECT max(org_seq) FROM audit_log WHERE org_id = $1", orgID).Scan(&maxSeq); err != nil {
		t.Fatal(err)
	}
	if maxSeq != 7 { // 3 provision events + 4 touches
		t.Errorf("max org_seq = %d, want 7 (contiguous per-org sequence)", maxSeq)
	}
}

func TestUserEmailUniqueness(t *testing.T) {
	pool := testPool(t)
	repos := identity.NewPostgresRepositories(pool)
	ctx := context.Background()

	email := uniqueSlug("case") + "@Example.com"
	u1, err := identity.NewUser(email, "First")
	if err != nil {
		t.Fatal(err)
	}
	tx, _ := pool.Begin(ctx)
	if err := repos.Users().Create(ctx, tx, u1); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	// citext: same address with different case must collide.
	u2, err := identity.NewUser(strings.ToLower(email), "Second")
	if err != nil {
		t.Fatal(err)
	}
	tx, _ = pool.Begin(ctx)
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repos.Users().Create(ctx, tx, u2); !errors.Is(err, identity.ErrAlreadyExists) {
		t.Fatalf("case-insensitive duplicate email: error = %v, want ErrAlreadyExists", err)
	}
}
