package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/noisyloop/legal-stack/backend/internal/platform/config"
)

func testConfig() config.Config {
	return config.Config{
		Env:               config.EnvTest,
		HTTPAddr:          ":0",
		MaxRequestBodyMiB: 1,
		RateLimitRPS:      1000,
		RateLimitBurst:    1000,
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

func newTestServer(t *testing.T, cfg config.Config, deps map[string]Pinger) http.Handler {
	t.Helper()
	return New(Options{Config: cfg, Logger: testLogger(), ReadinessChecks: deps}).Handler
}

func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	h := newTestServer(t, testConfig(), nil)

	for _, path := range []string{"/healthz", "/nonexistent"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		want := map[string]string{
			"X-Content-Type-Options":  "nosniff",
			"X-Frame-Options":         "DENY",
			"Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'",
			"Referrer-Policy":         "no-referrer",
			"Cache-Control":           "no-store",
		}
		for header, value := range want {
			if got := rec.Header().Get(header); got != value {
				t.Errorf("%s: header %s = %q, want %q", path, header, got, value)
			}
		}
	}
}

func TestRequestIDAssignedAndUnforgeable(t *testing.T) {
	h := newTestServer(t, testConfig(), nil)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-Id", "attacker-chosen")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	got := rec.Header().Get("X-Request-Id")
	if got == "" || got == "attacker-chosen" {
		t.Fatalf("X-Request-Id = %q: must be server-generated, never client input", got)
	}
	if len(got) != 32 {
		t.Errorf("X-Request-Id length = %d, want 32 hex chars", len(got))
	}
}

func TestCorrelationIDValidation(t *testing.T) {
	h := newTestServer(t, testConfig(), nil)

	valid := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	valid.Header.Set("X-Correlation-Id", "trace-123.abc")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, valid)
	if got := rec.Header().Get("X-Correlation-Id"); got != "trace-123.abc" {
		t.Errorf("valid correlation ID not echoed, got %q", got)
	}

	injection := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	injection.Header.Set("X-Correlation-Id", "bad\nvalue with spaces")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, injection)
	if got := rec.Header().Get("X-Correlation-Id"); got != "" {
		t.Errorf("malformed correlation ID must be dropped, got %q", got)
	}
}

func TestProblemJSONNotFound(t *testing.T) {
	h := newTestServer(t, testConfig(), nil)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", ct)
	}
	var p Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("body is not a Problem: %v", err)
	}
	if p.RequestID == "" {
		t.Error("problem body must carry request_id for support correlation")
	}
}

func TestReadyzReflectsDependencyHealth(t *testing.T) {
	healthy := newTestServer(t, testConfig(), map[string]Pinger{"database": fakePinger{}})
	rec := httptest.NewRecorder()
	healthy.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("healthy readyz = %d, want 200", rec.Code)
	}

	broken := newTestServer(t, testConfig(), map[string]Pinger{"database": fakePinger{err: errors.New("conn refused")}})
	rec = httptest.NewRecorder()
	broken.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("broken readyz = %d, want 503", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "conn refused") {
		t.Error("readyz body must not leak internal error detail")
	}
	if !strings.Contains(rec.Body.String(), "unavailable") {
		t.Error("readyz body should name the unavailable component")
	}
}

func TestLivenessIgnoresDependencies(t *testing.T) {
	broken := newTestServer(t, testConfig(), map[string]Pinger{"database": fakePinger{err: errors.New("down")}})
	rec := httptest.NewRecorder()
	broken.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("healthz = %d, want 200 even with dependencies down (no restart storms)", rec.Code)
	}
}

func TestRateLimitReturns429(t *testing.T) {
	cfg := testConfig()
	cfg.RateLimitRPS = 1
	cfg.RateLimitBurst = 2
	h := newTestServer(t, cfg, nil)

	var last int
	for i := 0; i < 5; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.RemoteAddr = "203.0.113.7:1234"
		h.ServeHTTP(rec, req)
		last = rec.Code
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("status after burst = %d, want 429", last)
	}
}

func TestBodyLimitRejectsOversized(t *testing.T) {
	h := newTestServer(t, testConfig(), nil)

	body := strings.NewReader(strings.Repeat("a", 2*1024*1024))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/anything", body)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}

func TestRealIPTrustsOnlyConfiguredProxies(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}

	var seen string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = ClientIPFrom(r.Context())
	})
	h := RealIP(trusted)(inner)

	// Peer is a trusted proxy: forwarded header honored.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.1.2.3:555"
	req.Header.Set("X-Forwarded-For", "198.51.100.9")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if seen != "198.51.100.9" {
		t.Errorf("trusted proxy: client ip = %q, want 198.51.100.9", seen)
	}

	// Peer is NOT trusted: forwarded header must be ignored (spoofing).
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.50:555"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if seen != "203.0.113.50" {
		t.Errorf("untrusted peer: client ip = %q, want 203.0.113.50 (header ignored)", seen)
	}
}

func TestRecoverConvertsPanicTo500(t *testing.T) {
	h := Recover(testLogger())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	// Request ID middleware not present here; Recover must still respond.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "boom") {
		t.Error("panic detail must not reach the client")
	}
}
