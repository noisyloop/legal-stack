package audit

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"time"

	"github.com/jackc/pgx/v5"
)

// metadataJSON serializes event metadata for the jsonb column. The column is
// NOT NULL DEFAULT '{}', so empty metadata is stored as an empty object.
func metadataJSON(m map[string]string) ([]byte, error) {
	if len(m) == 0 {
		return []byte("{}"), nil
	}
	out, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("audit: marshal metadata: %w", err)
	}
	return out, nil
}

// scanEvent reads one audit_log row in the column order used by
// VerifyOrgChain's query.
func scanEvent(rows pgx.Rows) (*Event, error) {
	var (
		e             Event
		occurredAt    time.Time
		actorType     string
		action        string
		metadataRaw   []byte
		ipText        *string
		userAgent     *string
		requestID     *string
		correlationID *string
	)
	if err := rows.Scan(
		&e.ID, &occurredAt, &e.OrgID, &e.OrgSeq, &e.MatterID,
		&actorType, &e.ActorUserID, &action, &e.ResourceType, &e.ResourceID,
		&e.OldValues, &e.NewValues, &metadataRaw,
		&ipText, &userAgent, &requestID, &correlationID,
		&e.PrevHash, &e.Hash,
	); err != nil {
		return nil, fmt.Errorf("audit: scan event: %w", err)
	}

	e.OccurredAt = occurredAt.UTC()
	e.ActorType = ActorType(actorType)
	e.Action = Action(action)

	if len(metadataRaw) > 0 {
		var m map[string]string
		if err := json.Unmarshal(metadataRaw, &m); err != nil {
			return nil, fmt.Errorf("audit: unmarshal metadata: %w", err)
		}
		if len(m) > 0 {
			e.Metadata = m
		}
	}
	if ipText != nil {
		addr, err := netip.ParseAddr(*ipText)
		if err != nil {
			return nil, fmt.Errorf("audit: parse stored ip: %w", err)
		}
		e.IP = &addr
	}
	if userAgent != nil {
		e.UserAgent = *userAgent
	}
	if requestID != nil {
		e.RequestID = *requestID
	}
	if correlationID != nil {
		e.CorrelationID = *correlationID
	}
	// Normalize JSON null representations: an absent jsonb value scans as
	// nil, matching how the hash treated it at append time.
	e.OldValues = normalizeRaw(e.OldValues)
	e.NewValues = normalizeRaw(e.NewValues)
	return &e, nil
}

func normalizeRaw(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return raw
}
