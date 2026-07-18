// Package audit implements the immutable, hash-chained audit trail
// (ADR-0006). Every module records state changes through this package, in
// the same database transaction as the change itself.
package audit

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/google/uuid"
)

// ActorType identifies who (or what) performed an action.
type ActorType string

const (
	ActorUser    ActorType = "user"
	ActorSystem  ActorType = "system"  // retention sweeps, chain verifier, migrations
	ActorService ActorType = "service" // future extracted services / integrations
)

// Action names follow "resource.verb" (e.g. "organization.create"). The
// catalog grows per module; format is enforced here so the audit stream
// stays machine-filterable.
type Action string

// Phase 1 actions.
const (
	ActionOrganizationCreate Action = "organization.create"
	ActionUserCreate         Action = "user.create"
	ActionMembershipCreate   Action = "membership.create"
)

// Event is one audit record. Fields set by the recorder (chain position,
// hashes) are exported for reads/verification but ignored on append.
type Event struct {
	ID         uuid.UUID
	OccurredAt time.Time
	// OrgID scopes the event to a tenant chain. Nil = system-wide event,
	// chained under the zero UUID.
	OrgID         *uuid.UUID
	OrgSeq        int64
	MatterID      *uuid.UUID
	ActorType     ActorType
	ActorUserID   *uuid.UUID
	Action        Action
	ResourceType  string
	ResourceID    *uuid.UUID
	OldValues     json.RawMessage
	NewValues     json.RawMessage
	Metadata      map[string]string
	IP            *netip.Addr
	UserAgent     string
	RequestID     string
	CorrelationID string
	PrevHash      []byte
	Hash          []byte
}

// Validate checks the invariants an event must satisfy before it may be
// appended. Rejecting bad events loudly beats storing an unusable trail.
func (e *Event) Validate() error {
	var problems []error
	if e.Action == "" {
		problems = append(problems, errors.New("action is required"))
	}
	if e.ResourceType == "" {
		problems = append(problems, errors.New("resource_type is required"))
	}
	switch e.ActorType {
	case ActorUser:
		if e.ActorUserID == nil {
			problems = append(problems, errors.New("actor_user_id is required for user actors"))
		}
	case ActorSystem, ActorService:
	default:
		problems = append(problems, fmt.Errorf("unknown actor_type %q", e.ActorType))
	}
	if len(problems) > 0 {
		return fmt.Errorf("audit: invalid event: %w", errors.Join(problems...))
	}
	return nil
}

// ChainKey returns the identifier of the hash chain this event belongs to:
// the org ID, or the zero UUID for system-wide events. Per-org chains keep
// append serialization per-tenant and let one tenant's history be verified
// (or exported for discovery) in isolation.
func (e *Event) ChainKey() uuid.UUID {
	if e.OrgID != nil {
		return *e.OrgID
	}
	return uuid.Nil
}
