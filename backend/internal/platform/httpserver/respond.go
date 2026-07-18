package httpserver

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// Problem is an RFC 7807 error body. Every error response uses this shape so
// clients have exactly one error contract — and so no handler is ever
// tempted to serialize an internal error object to the wire.
type Problem struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Detail    string `json:"detail,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

const problemTypeBase = "https://legal-stack.dev/errors/"

// WriteProblem writes an RFC 7807 response. detail must already be safe for
// external eyes — internal error text belongs in logs, keyed by request ID.
func WriteProblem(w http.ResponseWriter, r *http.Request, status int, kind, title, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	p := Problem{
		Type:      problemTypeBase + kind,
		Title:     title,
		Status:    status,
		Detail:    detail,
		RequestID: RequestIDFrom(r.Context()),
	}
	if err := json.NewEncoder(w).Encode(p); err != nil {
		slog.Default().ErrorContext(r.Context(), "write problem response", "error", err)
	}
}

// WriteJSON writes a success response body.
func WriteJSON(w http.ResponseWriter, r *http.Request, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Default().ErrorContext(r.Context(), "write json response", "error", err)
	}
}
