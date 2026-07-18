package identity

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/noisyloop/legal-stack/backend/internal/audit"
)

// Service is the identity application layer. It owns transaction boundaries:
// every state change and its audit record commit atomically (ADR-0006).
// This use case is the reference pattern all future modules follow.
type Service struct {
	pool        *pgxpool.Pool
	orgs        OrganizationRepository
	users       UserRepository
	memberships MembershipRepository
	recorder    *audit.Recorder
}

func NewService(pool *pgxpool.Pool, repos *PostgresRepositories, recorder *audit.Recorder) *Service {
	return &Service{
		pool:        pool,
		orgs:        repos,
		users:       repos.Users(),
		memberships: repos.Memberships(),
		recorder:    recorder,
	}
}

// ProvisionOrganizationCommand bootstraps a tenant with its owning user.
// In Phase 1 this is invoked by platform administration (actor: system);
// the HTTP surface arrives with authentication in Phase 2 — exposing a
// tenant-creating endpoint before authN exists would be indefensible.
type ProvisionOrganizationCommand struct {
	Slug       string
	Name       string
	Kind       OrgKind
	OwnerEmail string
	OwnerName  string
	// Request metadata for the audit trail.
	RequestID     string
	CorrelationID string
}

// ProvisionOrganizationResult reports what was created.
type ProvisionOrganizationResult struct {
	Organization *Organization
	Owner        *User
	Membership   *Membership
}

// ProvisionOrganization creates the org, its owner account, and the owner
// membership in one transaction, with one audit event per state change.
func (s *Service) ProvisionOrganization(ctx context.Context, cmd ProvisionOrganizationCommand) (*ProvisionOrganizationResult, error) {
	org, err := NewOrganization(cmd.Slug, cmd.Name, cmd.Kind)
	if err != nil {
		return nil, err
	}
	owner, err := NewUser(cmd.OwnerEmail, cmd.OwnerName)
	if err != nil {
		return nil, err
	}
	membership, err := NewMembership(org.ID, owner.ID, RoleOrgOwner, nil)
	if err != nil {
		return nil, err
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("identity: begin provision: %w", err)
	}
	// Rollback after commit is a no-op; this guards every early return.
	defer func() { _ = tx.Rollback(ctx) }()

	if err := s.orgs.Create(ctx, tx, org); err != nil {
		return nil, err
	}
	if err := s.users.Create(ctx, tx, owner); err != nil {
		return nil, err
	}
	if err := s.memberships.Create(ctx, tx, membership); err != nil {
		return nil, err
	}

	// Audit each state change inside the same transaction. NewValues carry
	// business-relevant fields only — never secrets, and only what the
	// domain object itself exposes.
	events := []*audit.Event{
		{
			OrgID: &org.ID, ActorType: audit.ActorSystem,
			Action: audit.ActionOrganizationCreate, ResourceType: "organization", ResourceID: &org.ID,
			NewValues: mustJSON(map[string]string{"slug": org.Slug, "name": org.Name, "kind": string(org.Kind)}),
			RequestID: cmd.RequestID, CorrelationID: cmd.CorrelationID,
		},
		{
			OrgID: &org.ID, ActorType: audit.ActorSystem,
			Action: audit.ActionUserCreate, ResourceType: "user", ResourceID: &owner.ID,
			NewValues: mustJSON(map[string]string{"email": owner.Email, "display_name": owner.DisplayName}),
			RequestID: cmd.RequestID, CorrelationID: cmd.CorrelationID,
		},
		{
			OrgID: &org.ID, ActorType: audit.ActorSystem,
			Action: audit.ActionMembershipCreate, ResourceType: "membership", ResourceID: &membership.ID,
			NewValues: mustJSON(map[string]string{"user_id": owner.ID.String(), "role": string(membership.Role)}),
			RequestID: cmd.RequestID, CorrelationID: cmd.CorrelationID,
		},
	}
	for _, e := range events {
		if err := s.recorder.Append(ctx, tx, e); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("identity: commit provision: %w", err)
	}
	return &ProvisionOrganizationResult{Organization: org, Owner: owner, Membership: membership}, nil
}

// mustJSON marshals audit payloads built from plain string maps; these
// cannot fail at runtime, and a panic here would indicate a programming
// error worth crashing loudly on.
func mustJSON(v map[string]string) json.RawMessage {
	out, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("identity: marshal audit payload: %v", err))
	}
	return out
}
