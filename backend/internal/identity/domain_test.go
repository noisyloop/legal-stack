package identity

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestNewOrganization(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		org, err := NewOrganization("  Acme-Law ", "  Acme Law LLP ", OrgKindLawFirm)
		if err != nil {
			t.Fatalf("NewOrganization() error = %v", err)
		}
		if org.Slug != "acme-law" {
			t.Errorf("slug = %q, want normalized lowercase", org.Slug)
		}
		if org.Name != "Acme Law LLP" {
			t.Errorf("name = %q, want trimmed", org.Name)
		}
		if org.Status != OrgStatusActive {
			t.Errorf("status = %q, want active", org.Status)
		}
		if org.ID == uuid.Nil {
			t.Error("ID must be generated")
		}
		if v := org.ID.Version(); v != 7 {
			t.Errorf("ID version = %d, want UUIDv7 (ADR-0007)", v)
		}
	})

	invalid := []struct {
		name, slug, orgName string
		kind                OrgKind
	}{
		{"slug too short", "ab", "Acme", OrgKindLawFirm},
		{"slug bad chars", "acme_law!", "Acme", OrgKindLawFirm},
		{"slug leading hyphen", "-acme", "Acme", OrgKindLawFirm},
		{"empty name", "acme-law", "   ", OrgKindLawFirm},
		{"unknown kind", "acme-law", "Acme", OrgKind("cartel")},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewOrganization(tc.slug, tc.orgName, tc.kind)
			if !errors.Is(err, ErrInvalidArgument) {
				t.Fatalf("error = %v, want ErrInvalidArgument", err)
			}
		})
	}
}

func TestNewUser(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		u, err := NewUser("counsel@acme.law", "Jordan Counsel")
		if err != nil {
			t.Fatalf("NewUser() error = %v", err)
		}
		if u.Status != UserStatusInvited {
			t.Errorf("status = %q, want invited until passkey enrollment", u.Status)
		}
	})

	invalid := []struct{ name, email, display string }{
		{"not an email", "not-an-email", "Jordan"},
		{"email with display part", "Jordan <j@acme.law>", "Jordan"},
		{"empty display name", "j@acme.law", "  "},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewUser(tc.email, tc.display); !errors.Is(err, ErrInvalidArgument) {
				t.Fatalf("error = %v, want ErrInvalidArgument", err)
			}
		})
	}
}

func TestNewMembership(t *testing.T) {
	orgID, userID := uuid.New(), uuid.New()

	m, err := NewMembership(orgID, userID, RoleAttorney, nil)
	if err != nil {
		t.Fatalf("NewMembership() error = %v", err)
	}
	if m.OrgID != orgID || m.UserID != userID {
		t.Error("membership must carry org and user")
	}

	if _, err := NewMembership(uuid.Nil, userID, RoleAttorney, nil); !errors.Is(err, ErrInvalidArgument) {
		t.Error("nil org must be rejected")
	}
	if _, err := NewMembership(orgID, userID, Role("superuser"), nil); !errors.Is(err, ErrInvalidArgument) {
		t.Error("unknown role must be rejected")
	}
}
