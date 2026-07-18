package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestRedactionOfSensitiveKeys(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{ReplaceAttr: redactSensitive}))

	logger.Info("login",
		"session_token", "super-secret-value",
		"Authorization", "Bearer abc",
		"user_id", "u-123",
	)

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("log output is not JSON: %v", err)
	}
	if record["session_token"] != redactedValue {
		t.Errorf("session_token = %q, want redacted", record["session_token"])
	}
	if record["Authorization"] != redactedValue {
		t.Errorf("Authorization = %q, want redacted (case-insensitive match)", record["Authorization"])
	}
	if record["user_id"] != "u-123" {
		t.Errorf("user_id = %q, non-sensitive keys must pass through", record["user_id"])
	}
	if bytes.Contains(buf.Bytes(), []byte("super-secret-value")) {
		t.Error("secret value leaked into log output")
	}
}
