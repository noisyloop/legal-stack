// Package identity owns the tenancy core: organizations, users, and the
// memberships that anchor RBAC. Domain layer — no HTTP, no SQL.
package identity

import (
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Domain errors. Callers branch on these with errors.Is; the HTTP layer maps
// them to problem+json without leaking internals.
var (
	ErrInvalidArgument = errors.New("invalid argument")
	ErrNotFound        = errors.New("not found")
	ErrAlreadyExists   = errors.New("already exists")
)

// OrgKind classifies the tenant.
type OrgKind string

const (
	OrgKindLawFirm    OrgKind = "law_firm"
	OrgKindClientOrg  OrgKind = "client_org"
	OrgKindManagement OrgKind = "management"
	OrgKindLabel      OrgKind = "label"
	OrgKindPersonal   OrgKind = "personal"
)

func (k OrgKind) valid() bool {
	switch k {
	case OrgKindLawFirm, OrgKindClientOrg, OrgKindManagement, OrgKindLabel, OrgKindPersonal:
		return true
	}
	return false
}

// OrgStatus is the organization lifecycle state.
type OrgStatus string

const (
	OrgStatusActive      OrgStatus = "active"
	OrgStatusSuspended   OrgStatus = "suspended"
	OrgStatusOffboarding OrgStatus = "offboarding"
	OrgStatusClosed      OrgStatus = "closed"
)

// Role is a membership role — the RBAC anchor (see security model).
type Role string

const (
	RoleOrgOwner Role = "org_owner"
	RoleOrgAdmin Role = "org_admin"
	RoleAttorney Role = "attorney"
	RoleStaff    Role = "staff"
	RoleClient   Role = "client"
	RoleGuest    Role = "guest"
)

func (r Role) valid() bool {
	switch r {
	case RoleOrgOwner, RoleOrgAdmin, RoleAttorney, RoleStaff, RoleClient, RoleGuest:
		return true
	}
	return false
}

// UserStatus is the account lifecycle state.
type UserStatus string

const (
	UserStatusInvited     UserStatus = "invited"
	UserStatusActive      UserStatus = "active"
	UserStatusSuspended   UserStatus = "suspended"
	UserStatusDeactivated UserStatus = "deactivated"
)

// Organization is the tenancy root.
type Organization struct {
	ID        uuid.UUID
	Slug      string
	Name      string
	Kind      OrgKind
	Status    OrgStatus
	CreatedAt time.Time
	UpdatedAt time.Time
}

// User is a global account; org access flows only through memberships.
type User struct {
	ID              uuid.UUID
	Email           string
	EmailVerifiedAt *time.Time
	DisplayName     string
	Status          UserStatus
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Membership binds a user to an organization with a role.
type Membership struct {
	ID        uuid.UUID
	OrgID     uuid.UUID
	UserID    uuid.UUID
	Role      Role
	Status    string
	InvitedBy *uuid.UUID
	CreatedAt time.Time
	UpdatedAt time.Time
}

// slugPattern mirrors the database CHECK constraint: validation lives in
// both layers on purpose — the domain gives good errors, the DB guarantees
// no bypass path exists.
var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`)

// NewOrganization constructs a valid organization or reports why it can't.
func NewOrganization(slug, name string, kind OrgKind) (*Organization, error) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	name = strings.TrimSpace(name)

	var problems []error
	if !slugPattern.MatchString(slug) {
		problems = append(problems, errors.New("slug must be 3-63 chars of lowercase letters, digits, and hyphens, not starting or ending with a hyphen"))
	}
	if l := len(name); l < 1 || l > 200 {
		problems = append(problems, errors.New("name must be 1-200 characters"))
	}
	if !kind.valid() {
		problems = append(problems, fmt.Errorf("kind %q is not recognized", kind))
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, errors.Join(problems...))
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("identity: generate id: %w", err)
	}
	return &Organization{
		ID:     id,
		Slug:   slug,
		Name:   name,
		Kind:   kind,
		Status: OrgStatusActive,
	}, nil
}

// NewUser constructs a valid user in the invited state (activation happens
// when the user completes passkey enrollment — Phase 2).
func NewUser(email, displayName string) (*User, error) {
	email = strings.TrimSpace(email)
	displayName = strings.TrimSpace(displayName)

	var problems []error
	if addr, err := mail.ParseAddress(email); err != nil || addr.Address != email || len(email) > 320 {
		problems = append(problems, errors.New("email is not a valid address"))
	}
	if l := len(displayName); l < 1 || l > 200 {
		problems = append(problems, errors.New("display name must be 1-200 characters"))
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, errors.Join(problems...))
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("identity: generate id: %w", err)
	}
	return &User{
		ID:          id,
		Email:       email,
		DisplayName: displayName,
		Status:      UserStatusInvited,
	}, nil
}

// NewMembership binds a user to an org with a role.
func NewMembership(orgID, userID uuid.UUID, role Role, invitedBy *uuid.UUID) (*Membership, error) {
	if orgID == uuid.Nil || userID == uuid.Nil {
		return nil, fmt.Errorf("%w: org and user are required", ErrInvalidArgument)
	}
	if !role.valid() {
		return nil, fmt.Errorf("%w: role %q is not recognized", ErrInvalidArgument, role)
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("identity: generate id: %w", err)
	}
	return &Membership{
		ID:        id,
		OrgID:     orgID,
		UserID:    userID,
		Role:      role,
		Status:    "active",
		InvitedBy: invitedBy,
	}, nil
}
