package identity

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Repositories are ports: the domain/application layers declare what they
// need; infrastructure (postgres.go) provides it. Mutating methods take
// pgx.Tx so use cases control transaction boundaries — a use case, not a
// repository, decides what must commit atomically (state + audit).

type OrganizationRepository interface {
	Create(ctx context.Context, tx pgx.Tx, org *Organization) error
	GetByID(ctx context.Context, id uuid.UUID) (*Organization, error)
	GetBySlug(ctx context.Context, slug string) (*Organization, error)
}

type UserRepository interface {
	Create(ctx context.Context, tx pgx.Tx, user *User) error
	GetByID(ctx context.Context, id uuid.UUID) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
}

type MembershipRepository interface {
	Create(ctx context.Context, tx pgx.Tx, m *Membership) error
	ListByUser(ctx context.Context, userID uuid.UUID) ([]*Membership, error)
}
