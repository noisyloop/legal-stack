// Package logging configures structured logging (slog) with centralized
// secret redaction. Redaction lives here — at the sink — rather than at call
// sites, because a rule enforced in one place cannot be forgotten in others.
package logging

import (
	"log/slog"
	"os"
	"strings"
)

// sensitiveKeySubstrings marks attribute keys whose values must never reach
// a log sink, regardless of where the log call originates.
var sensitiveKeySubstrings = []string{
	"password", "secret", "token", "authorization", "cookie",
	"credential", "recovery_code", "private_key", "api_key",
}

const redactedValue = "[REDACTED]"

// New builds the process-wide logger.
//
// format: "json" (machine ingestion: Loki, CloudWatch) or "text" (humans).
// level: debug | info | warn | error.
func New(format, level string) *slog.Logger {
	opts := &slog.HandlerOptions{
		Level:       parseLevel(level),
		ReplaceAttr: redactSensitive,
	}

	var handler slog.Handler
	if format == "text" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}
	return slog.New(handler)
}

func parseLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// redactSensitive replaces values for keys that look secret-bearing. It runs
// on every attribute of every record, so the cost must stay trivial: a few
// substring checks on the key only (values are never inspected).
func redactSensitive(_ []string, a slog.Attr) slog.Attr {
	key := strings.ToLower(a.Key)
	for _, s := range sensitiveKeySubstrings {
		if strings.Contains(key, s) {
			a.Value = slog.StringValue(redactedValue)
			return a
		}
	}
	return a
}
