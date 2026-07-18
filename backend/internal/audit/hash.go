package audit

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/uuid"
)

// Hash chain construction:
//
//	hash(n) = SHA-256( prev_hash(n) || canonical(event_n) )
//
// where canonical() is a length-prefixed, fixed-order serialization of the
// event's immutable fields. Length prefixes remove ambiguity — with plain
// concatenation, ("ab","c") and ("a","bc") would hash identically, letting
// an attacker move bytes between fields without breaking the chain.
//
// The chain makes the log tamper-EVIDENT: modifying, inserting, or deleting
// any historical row breaks every subsequent hash in that org's chain.
// Detection is the verifier's job (VerifyChain + WORM checkpoints).

// GenesisHash anchors the first event of every chain: 32 zero bytes.
var GenesisHash = make([]byte, sha256.Size)

// ComputeHash returns SHA-256(prevHash || canonical(event)).
func ComputeHash(prevHash []byte, e *Event) ([]byte, error) {
	payload, err := canonicalPayload(e)
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	h.Write(prevHash)
	h.Write(payload)
	return h.Sum(nil), nil
}

// canonicalPayload serializes the immutable event fields in a fixed order.
// Any change to this order or field set is a breaking change to chain
// verification and requires a versioned migration strategy — hence the
// leading format-version byte.
func canonicalPayload(e *Event) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte(0x01) // canonical format version

	writeField := func(b []byte) {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(b)))
		buf.Write(length[:])
		buf.Write(b)
	}
	writeString := func(s string) { writeField([]byte(s)) }

	writeField(e.ID[:])
	// Truncate to microseconds: PostgreSQL timestamptz stores microsecond
	// precision, so hashing nanoseconds would break verification round-trips.
	writeField(binary.BigEndian.AppendUint64(nil, uint64(e.OccurredAt.UTC().Truncate(1000).UnixNano()))) // #nosec G115 -- two's-complement encoding of the signed value is intentional and lossless
	chain := e.ChainKey()
	writeField(chain[:])
	writeField(binary.BigEndian.AppendUint64(nil, uint64(e.OrgSeq))) // #nosec G115 -- org_seq >= 1 by CHECK constraint; encoding is lossless
	writeString(string(e.ActorType))
	writeOptionalUUID(writeField, e.ActorUserID)
	writeString(string(e.Action))
	writeString(e.ResourceType)
	writeOptionalUUID(writeField, e.ResourceID)
	writeOptionalUUID(writeField, e.MatterID)
	// Old/new values are hashed as raw bytes; the schema stores them in
	// json (not jsonb) columns precisely so those bytes round-trip verbatim.
	writeField(e.OldValues)
	writeField(e.NewValues)

	metadata, err := canonicalMetadata(e.Metadata)
	if err != nil {
		return nil, err
	}
	writeField(metadata)

	if e.IP != nil {
		writeString(e.IP.String())
	} else {
		writeString("")
	}
	writeString(e.UserAgent)
	writeString(e.RequestID)
	writeString(e.CorrelationID)

	return buf.Bytes(), nil
}

func writeOptionalUUID(writeField func([]byte), id *uuid.UUID) {
	if id != nil {
		writeField(id[:])
	} else {
		writeField(nil)
	}
}

// canonicalMetadata serializes metadata with sorted keys so hashing is
// deterministic regardless of map iteration order.
func canonicalMetadata(m map[string]string) ([]byte, error) {
	if len(m) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf bytes.Buffer
	for _, k := range keys {
		entry, err := json.Marshal([2]string{k, m[k]})
		if err != nil {
			return nil, fmt.Errorf("audit: marshal metadata: %w", err)
		}
		buf.Write(entry)
	}
	return buf.Bytes(), nil
}

// VerifyChain checks a contiguous slice of one chain's events (ascending
// OrgSeq). prevHash is the hash preceding the first event — GenesisHash when
// verifying from the start. It returns the first break found.
func VerifyChain(prevHash []byte, events []*Event) error {
	expectedSeq := int64(0)
	if len(events) > 0 {
		expectedSeq = events[0].OrgSeq
	}
	for i, e := range events {
		if e.OrgSeq != expectedSeq {
			return fmt.Errorf("audit: chain gap at index %d: org_seq %d, want %d", i, e.OrgSeq, expectedSeq)
		}
		if subtle.ConstantTimeCompare(e.PrevHash, prevHash) != 1 {
			return fmt.Errorf("audit: chain break at org_seq %d: prev_hash mismatch", e.OrgSeq)
		}
		computed, err := ComputeHash(prevHash, e)
		if err != nil {
			return err
		}
		if subtle.ConstantTimeCompare(e.Hash, computed) != 1 {
			return fmt.Errorf("audit: chain break at org_seq %d: stored hash does not match event contents", e.OrgSeq)
		}
		prevHash = e.Hash
		expectedSeq++
	}
	return nil
}
