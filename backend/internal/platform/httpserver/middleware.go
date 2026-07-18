package httpserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type contextKey string

const (
	ctxKeyRequestID     contextKey = "request_id"
	ctxKeyCorrelationID contextKey = "correlation_id"
	ctxKeyClientIP      contextKey = "client_ip"
)

// RequestIDFrom returns the server-assigned request ID, or "".
func RequestIDFrom(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyRequestID).(string)
	return v
}

// CorrelationIDFrom returns the validated client correlation ID, or "".
func CorrelationIDFrom(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyCorrelationID).(string)
	return v
}

// ClientIPFrom returns the resolved client IP, or "".
func ClientIPFrom(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyClientIP).(string)
	return v
}

// RequestID assigns a fresh server-generated ID to every request. The ID is
// never taken from the client: it is used in audit records and support
// tooling, so it must be unforgeable and collision-free.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 16)
		if _, err := rand.Read(buf); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		id := hex.EncodeToString(buf)
		ctx := context.WithValue(r.Context(), ctxKeyRequestID, id)
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// correlationIDPattern constrains client-supplied correlation IDs: they end
// up in logs and audit rows, so shape and length are enforced (log-injection
// and storage-abuse hygiene).
var correlationIDPattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)

// CorrelationID accepts a client-supplied X-Correlation-Id if well-formed.
func CorrelationID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := r.Header.Get("X-Correlation-Id"); id != "" && correlationIDPattern.MatchString(id) {
			ctx := context.WithValue(r.Context(), ctxKeyCorrelationID, id)
			w.Header().Set("X-Correlation-Id", id)
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}

// RealIP resolves the client IP. X-Forwarded-For is only honored when the
// direct peer is inside a configured trusted-proxy CIDR — otherwise any
// client could spoof its IP and bypass IP-keyed rate limits and audit.
func RealIP(trustedProxies []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := peerIP(r.RemoteAddr)
			if len(trustedProxies) > 0 && ipInPrefixes(ip, trustedProxies) {
				if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
					// Rightmost non-trusted hop is the client as seen by our
					// first trusted proxy; earlier entries are client-forgeable.
					hops := strings.Split(fwd, ",")
					for i := len(hops) - 1; i >= 0; i-- {
						candidate, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
						if err != nil {
							break // malformed header: fall back to peer address
						}
						if !ipInPrefixes(candidate, trustedProxies) {
							ip = candidate
							break
						}
					}
				}
			}
			ctx := context.WithValue(r.Context(), ctxKeyClientIP, ip.String())
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func peerIP(remoteAddr string) netip.Addr {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return addr.Unmap()
}

func ipInPrefixes(ip netip.Addr, prefixes []netip.Prefix) bool {
	if !ip.IsValid() {
		return false
	}
	for _, p := range prefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// SecurityHeaders applies browser hardening on every response, including
// errors. The API serves JSON, so the CSP is maximally restrictive — it
// exists to neutralize any response that a browser is tricked into rendering.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// BodyLimit caps request body size before handlers read it. Oversized bodies
// are a memory-exhaustion vector; JSON endpoints never need more than a few
// KiB, and uploads use a dedicated multipart flow with its own limits.
func BodyLimit(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > maxBytes {
				WriteProblem(w, r, http.StatusRequestEntityTooLarge, "body-too-large",
					"Request body too large", "")
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

// Recover converts panics into clean 500 responses. Without it, one panic
// kills the connection mid-response and, under some handlers, leaks stack
// detail. The stack goes to logs, never to the client.
func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					if err, ok := rec.(error); ok && errors.Is(err, http.ErrAbortHandler) {
						panic(rec) // deliberate abort: propagate to net/http
					}
					logger.ErrorContext(r.Context(), "panic recovered",
						"panic", rec,
						"stack", string(debug.Stack()),
						"request_id", RequestIDFrom(r.Context()),
						"path", r.URL.Path,
					)
					WriteProblem(w, r, http.StatusInternalServerError, "internal",
						"Internal server error", "")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// ipRateLimiter is a per-client token bucket table. In-memory (per replica)
// is the right Phase 1 tool: it needs no shared infrastructure and degrades
// safely (limits multiply by replica count). Redis-backed distributed limits
// arrive with the auth module, where exact budgets matter.
type ipRateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*bucketEntry
	rps      rate.Limit
	burst    int
	maxSize  int
	lastSeen func() time.Time
}

type bucketEntry struct {
	limiter *rate.Limiter
	seen    time.Time
}

func newIPRateLimiter(rps float64, burst int) *ipRateLimiter {
	return &ipRateLimiter{
		buckets:  make(map[string]*bucketEntry),
		rps:      rate.Limit(rps),
		burst:    burst,
		maxSize:  100_000, // hard cap: unbounded per-IP state is itself a DoS vector
		lastSeen: time.Now,
	}
}

func (l *ipRateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) >= l.maxSize {
			l.evictOldestLocked()
		}
		entry = &bucketEntry{limiter: rate.NewLimiter(l.rps, l.burst)}
		l.buckets[key] = entry
	}
	entry.seen = l.lastSeen()
	return entry.limiter.Allow()
}

// evictOldestLocked drops the stalest 10% of entries. Linear scan is fine:
// it runs only when the table is at capacity, which implies an active flood,
// and 100k entries scan in well under a millisecond.
func (l *ipRateLimiter) evictOldestLocked() {
	type aged struct {
		key  string
		seen time.Time
	}
	oldest := make([]aged, 0, len(l.buckets))
	for k, v := range l.buckets {
		oldest = append(oldest, aged{k, v.seen})
	}
	cutoff := len(oldest) / 10
	if cutoff == 0 {
		cutoff = 1
	}
	for i := 0; i < cutoff; i++ {
		idx := i
		for j := i + 1; j < len(oldest); j++ {
			if oldest[j].seen.Before(oldest[idx].seen) {
				idx = j
			}
		}
		oldest[i], oldest[idx] = oldest[idx], oldest[i]
		delete(l.buckets, oldest[i].key)
	}
}

// RateLimit rejects clients exceeding the per-IP budget with 429.
func RateLimit(rps float64, burst int) func(http.Handler) http.Handler {
	limiter := newIPRateLimiter(rps, burst)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := ClientIPFrom(r.Context())
			if key == "" {
				key = r.RemoteAddr
			}
			if !limiter.allow(key) {
				w.Header().Set("Retry-After", "1")
				WriteProblem(w, r, http.StatusTooManyRequests, "rate-limited",
					"Too many requests", "")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// statusRecorder captures the response status for access logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (rec *statusRecorder) WriteHeader(code int) {
	rec.status = code
	rec.ResponseWriter.WriteHeader(code)
}

func (rec *statusRecorder) Write(b []byte) (int, error) {
	if rec.status == 0 {
		rec.status = http.StatusOK
	}
	return rec.ResponseWriter.Write(b)
}

// AccessLog emits one structured line per request with the fields the
// observability design mandates. Bodies and headers are never logged.
func AccessLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			if rec.status == 0 {
				rec.status = http.StatusOK
			}
			logger.InfoContext(r.Context(), "http_request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", RequestIDFrom(r.Context()),
				"correlation_id", CorrelationIDFrom(r.Context()),
				"client_ip", ClientIPFrom(r.Context()),
				"user_agent", r.UserAgent(),
			)
		})
	}
}
