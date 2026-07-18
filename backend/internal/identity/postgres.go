package identity

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresRepositories implements the identity ports against PostgreSQL.
// All SQL for this module lives in this file; explicit column lists only.
type PostgresRepositories struct {
	pool *pgxpool.Pool
}

func NewPostgresRepositories(pool *pgxpool.Pool) *PostgresRepositories {
	return &PostgresRepositories{pool: pool}
}

const uniqueViolation = "23505"

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation
}

// --- OrganizationRepository ---

func (r *PostgresRepositories) Create(ctx context.Context, tx pgx.Tx, org *Organization) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO organizations (id, slug, name, kind, status)
		VALUES ($1, $2, $3, $4, $5)`,
		org.ID, org.Slug, org.Name, string(org.Kind), string(org.Status))
	if isUniqueViolation(err) {
		return fmt.Errorf("%w: organization slug %q", ErrAlreadyExists, org.Slug)
	}
	if err != nil {
		return fmt.Errorf("identity: create organization: %w", err)
	}
	return nil
}

func (r *PostgresRepositories) GetByID(ctx context.Context, id uuid.UUID) (*Organization, error) {
	return r.scanOrg(r.pool.QueryRow(ctx, `
		SELECT id, slug, name, kind, status, created_at, updated_at
		FROM organizations
		WHERE id = $1 AND deleted_at IS NULL`, id))
}

func (r *PostgresRepositories) GetBySlug(ctx context.Context, slug string) (*Organization, error) {
	return r.scanOrg(r.pool.QueryRow(ctx, `
		SELECT id, slug, name, kind, status, created_at, updated_at
		FROM organizations
		WHERE slug = $1 AND deleted_at IS NULL`, slug))
}

func (r *PostgresRepositories) scanOrg(row pgx.Row) (*Organization, error) {
	var org Organization
	var kind, status string
	err := row.Scan(&org.ID, &org.Slug, &org.Name, &kind, &status, &org.CreatedAt, &org.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: organization", ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("identity: get organization: %w", err)
	}
	org.Kind = OrgKind(kind)
	org.Status = OrgStatus(status)
	return &org, nil
}

// --- UserRepository ---

// Users implements UserRepository as a facet of the same struct; exposed
// separately so consumers depend only on the port they need.
type userRepo struct{ *PostgresRepositories }

func (r *PostgresRepositories) Users() UserRepository { return userRepo{r} }

func (r userRepo) Create(ctx context.Context, tx pgx.Tx, u *User) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO users (id, email, display_name, status)
		VALUES ($1, $2, $3, $4)`,
		u.ID, u.Email, u.DisplayName, string(u.Status))
	if isUniqueViolation(err) {
		return fmt.Errorf("%w: user email", ErrAlreadyExists)
	}
	if err != nil {
		return fmt.Errorf("identity: create user: %w", err)
	}
	return nil
}

func (r userRepo) GetByID(ctx context.Context, id uuid.UUID) (*User, error) {
	return r.scanUser(r.pool.QueryRow(ctx, `
		SELECT id, email, email_verified_at, display_name, status, created_at, updated_at
		FROM users
		WHERE id = $1 AND deleted_at IS NULL`, id))
}

func (r userRepo) GetByEmail(ctx context.Context, email string) (*User, error) {
	return r.scanUser(r.pool.QueryRow(ctx, `
		SELECT id, email, email_verified_at, display_name, status, created_at, updated_at
		FROM users
		WHERE email = $1 AND deleted_at IS NULL`, email))
}

func (r userRepo) scanUser(row pgx.Row) (*User, error) {
	var u User
	var status string
	err := row.Scan(&u.ID, &u.Email, &u.EmailVerifiedAt, &u.DisplayName, &status, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: user", ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("identity: get user: %w", err)
	}
	u.Status = UserStatus(status)
	return &u, nil
}

// --- MembershipRepository ---

type membershipRepo struct{ *PostgresRepositories }

func (r *PostgresRepositories) Memberships() MembershipRepository { return membershipRepo{r} }

func (r membershipRepo) Create(ctx context.Context, tx pgx.Tx, m *Membership) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO org_memberships (id, org_id, user_id, role, status, invited_by)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		m.ID, m.OrgID, m.UserID, string(m.Role), m.Status, m.InvitedBy)
	if isUniqueViolation(err) {
		return fmt.Errorf("%w: membership", ErrAlreadyExists)
	}
	if err != nil {
		return fmt.Errorf("identity: create membership: %w", err)
	}
	return nil
}

func (r membershipRepo) ListByUser(ctx context.Context, userID uuid.UUID) ([]*Membership, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, org_id, user_id, role, status, invited_by, created_at, updated_at
		FROM org_memberships
		WHERE user_id = $1 AND removed_at IS NULL
		ORDER BY created_at`, userID)
	if err != nil {
		return nil, fmt.Errorf("identity: list memberships: %w", err)
	}
	defer rows.Close()

	var out []*Membership
	for rows.Next() {
		var m Membership
		var role string
		if err := rows.Scan(&m.ID, &m.OrgID, &m.UserID, &role, &m.Status, &m.InvitedBy, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, fmt.Errorf("identity: scan membership: %w", err)
		}
		m.Role = Role(role)
		out = append(out, &m)
	}
	return out, rows.Err()
}
