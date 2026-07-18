package audit

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func testEvent(t *testing.T, orgID uuid.UUID, seq int64) *Event {
	t.Helper()
	actor := uuid.MustParse("018f0000-0000-7000-8000-00000000aaaa")
	return &Event{
		ID:           uuid.MustParse("018f0000-0000-7000-8000-000000000001"),
		OccurredAt:   time.Date(2026, 7, 18, 12, 0, 0, 123456000, time.UTC),
		OrgID:        &orgID,
		OrgSeq:       seq,
		ActorType:    ActorUser,
		ActorUserID:  &actor,
		Action:       ActionOrganizationCreate,
		ResourceType: "organization",
		NewValues:    json.RawMessage(`{"name":"Acme Law"}`),
		Metadata:     map[string]string{"b": "2", "a": "1"},
		RequestID:    "req-1",
	}
}

func buildChain(t *testing.T, orgID uuid.UUID, n int) []*Event {
	t.Helper()
	events := make([]*Event, 0, n)
	prev := GenesisHash
	for i := 0; i < n; i++ {
		// Deterministic IDs so identically-built chains are reproducible,
		// which lets tests compare chain heads meaningfully.
		e := testEvent(t, orgID, int64(i+1))
		e.RequestID = "req-" + string(rune('a'+i))
		e.PrevHash = prev
		hash, err := ComputeHash(prev, e)
		if err != nil {
			t.Fatalf("ComputeHash: %v", err)
		}
		e.Hash = hash
		prev = hash
		events = append(events, e)
	}
	return events
}

func TestComputeHashIsDeterministic(t *testing.T) {
	orgID := uuid.New()
	e := testEvent(t, orgID, 1)

	h1, err := ComputeHash(GenesisHash, e)
	if err != nil {
		t.Fatal(err)
	}
	// Metadata map iteration order must not affect the hash.
	for i := 0; i < 20; i++ {
		h2, err := ComputeHash(GenesisHash, e)
		if err != nil {
			t.Fatal(err)
		}
		if string(h1) != string(h2) {
			t.Fatal("hash is not deterministic across map iteration orders")
		}
	}
}

func TestComputeHashCoversEveryField(t *testing.T) {
	orgID := uuid.New()
	base := testEvent(t, orgID, 1)
	baseHash, err := ComputeHash(GenesisHash, base)
	if err != nil {
		t.Fatal(err)
	}

	mutations := map[string]func(*Event){
		"org_seq":        func(e *Event) { e.OrgSeq = 2 },
		"action":         func(e *Event) { e.Action = "organization.delete" },
		"resource_type":  func(e *Event) { e.ResourceType = "user" },
		"actor_type":     func(e *Event) { e.ActorType = ActorSystem; e.ActorUserID = nil },
		"new_values":     func(e *Event) { e.NewValues = json.RawMessage(`{"name":"Evil Law"}`) },
		"metadata":       func(e *Event) { e.Metadata = map[string]string{"a": "tampered"} },
		"request_id":     func(e *Event) { e.RequestID = "req-2" },
		"occurred_at":    func(e *Event) { e.OccurredAt = e.OccurredAt.Add(time.Second) },
		"correlation_id": func(e *Event) { e.CorrelationID = "other" },
		"user_agent":     func(e *Event) { e.UserAgent = "curl" },
	}
	for field, mutate := range mutations {
		t.Run(field, func(t *testing.T) {
			e := testEvent(t, orgID, 1)
			mutate(e)
			h, err := ComputeHash(GenesisHash, e)
			if err != nil {
				t.Fatal(err)
			}
			if string(h) == string(baseHash) {
				t.Errorf("changing %s did not change the hash — field is not covered", field)
			}
		})
	}
}

func TestFieldBoundaryAmbiguity(t *testing.T) {
	// Length prefixes must prevent moving bytes between adjacent fields.
	orgID := uuid.New()
	a := testEvent(t, orgID, 1)
	a.ResourceType = "ab"
	a.UserAgent = "" // isolate: only compare two adjacent string fields
	a.RequestID = "c"

	b := testEvent(t, orgID, 1)
	b.ResourceType = "a"
	b.UserAgent = ""
	b.RequestID = "bc"

	ha, _ := ComputeHash(GenesisHash, a)
	hb, _ := ComputeHash(GenesisHash, b)
	if string(ha) == string(hb) {
		t.Fatal("concatenation ambiguity: (ab, c) and (a, bc) hash identically")
	}
}

func TestVerifyChainAcceptsValidChain(t *testing.T) {
	events := buildChain(t, uuid.New(), 5)
	if err := VerifyChain(GenesisHash, events); err != nil {
		t.Fatalf("valid chain rejected: %v", err)
	}
}

func TestVerifyChainDetectsTampering(t *testing.T) {
	orgID := uuid.New()

	t.Run("modified event", func(t *testing.T) {
		events := buildChain(t, orgID, 5)
		events[2].NewValues = json.RawMessage(`{"name":"Tampered"}`)
		err := VerifyChain(GenesisHash, events)
		if err == nil || !strings.Contains(err.Error(), "org_seq 3") {
			t.Fatalf("tampered event not detected at the right position: %v", err)
		}
	})

	t.Run("deleted event", func(t *testing.T) {
		events := buildChain(t, orgID, 5)
		spliced := append(events[:2:2], events[3:]...)
		if err := VerifyChain(GenesisHash, spliced); err == nil {
			t.Fatal("deleted event not detected")
		}
	})

	t.Run("reordered events", func(t *testing.T) {
		events := buildChain(t, orgID, 5)
		events[1], events[2] = events[2], events[1]
		if err := VerifyChain(GenesisHash, events); err == nil {
			t.Fatal("reordered events not detected")
		}
	})

	t.Run("recomputed tail after edit", func(t *testing.T) {
		// Attacker edits event 3 AND recomputes hashes 3..5. Verification
		// from the genesis (or a stored checkpoint before the edit) fails
		// because event 3's prev_hash no longer matches event 2's hash...
		// unless they recompute from the break onward consistently, which is
		// why chain heads are checkpointed to WORM storage (ADR-0006).
		events := buildChain(t, orgID, 5)
		events[2].NewValues = json.RawMessage(`{"name":"Tampered"}`)
		prev := events[1].Hash
		for i := 2; i < 5; i++ {
			events[i].PrevHash = prev
			h, err := ComputeHash(prev, events[i])
			if err != nil {
				t.Fatal(err)
			}
			events[i].Hash = h
			prev = h
		}
		// The internally-consistent rewrite passes VerifyChain from genesis…
		if err := VerifyChain(GenesisHash, events); err != nil {
			t.Fatalf("internally consistent rewrite should verify: %v", err)
		}
		// …but the head no longer matches the checkpointed head.
		original := buildChain(t, orgID, 5)
		if string(original[4].Hash) == string(events[4].Hash) {
			t.Fatal("rewrite must change the chain head (checkpoint comparison detects it)")
		}
	})
}

func TestVerifyChainDetectsSeqGap(t *testing.T) {
	events := buildChain(t, uuid.New(), 3)
	events[2].OrgSeq = 5
	if err := VerifyChain(GenesisHash, events); err == nil || !strings.Contains(err.Error(), "gap") {
		t.Fatalf("sequence gap not detected: %v", err)
	}
}

func TestEventValidate(t *testing.T) {
	orgID := uuid.New()

	valid := testEvent(t, orgID, 1)
	if err := valid.Validate(); err != nil {
		t.Errorf("valid event rejected: %v", err)
	}

	missingActor := testEvent(t, orgID, 1)
	missingActor.ActorUserID = nil
	if err := missingActor.Validate(); err == nil {
		t.Error("user actor without actor_user_id must be rejected")
	}

	system := testEvent(t, orgID, 1)
	system.ActorType = ActorSystem
	system.ActorUserID = nil
	if err := system.Validate(); err != nil {
		t.Errorf("system event without user must be valid: %v", err)
	}

	noAction := testEvent(t, orgID, 1)
	noAction.Action = ""
	if err := noAction.Validate(); err == nil {
		t.Error("event without action must be rejected")
	}
}

func TestChainKey(t *testing.T) {
	orgID := uuid.New()
	withOrg := testEvent(t, orgID, 1)
	if withOrg.ChainKey() != orgID {
		t.Error("org event must chain under its org")
	}
	system := testEvent(t, orgID, 1)
	system.OrgID = nil
	if system.ChainKey() != uuid.Nil {
		t.Error("system event must chain under the zero UUID")
	}
}
