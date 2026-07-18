// Package httpserver assembles the hardened HTTP entry point: routing,
// middleware pipeline, health endpoints, and server timeouts. It contains no
// business logic — modules mount their handlers onto the router.
package httpserver

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/noisyloop/legal-stack/backend/internal/platform/config"
)

// Options wires the server's dependencies from the composition root.
type Options struct {
	Config config.Config
	Logger *slog.Logger
	// Readiness dependencies by component name (e.g. "database").
	ReadinessChecks map[string]Pinger
	// APIMounter lets modules register routes under /api/v1 without this
	// package importing them (dependencies point inward).
	APIMounter func(chi.Router)
}

// New builds the http.Server with the full middleware pipeline. Ordering is
// deliberate and load-bearing — see comments inline.
func New(opts Options) *http.Server {
	r := chi.NewRouter()

	// Outermost: identity of the request itself, then panic safety, then
	// everything that might reject the request. Recover sits above the rest
	// so a panic anywhere below still yields a clean, logged 500 with the
	// request ID already assigned.
	r.Use(RequestID)
	r.Use(CorrelationID)
	r.Use(Recover(opts.Logger))
	r.Use(RealIP(opts.Config.TrustedProxies))
	r.Use(SecurityHeaders)
	r.Use(AccessLog(opts.Logger))
	r.Use(RateLimit(opts.Config.RateLimitRPS, opts.Config.RateLimitBurst))
	r.Use(BodyLimit(opts.Config.MaxRequestBodyMiB * 1024 * 1024))

	r.Get("/healthz", handleHealthz())
	r.Get("/readyz", handleReadyz(opts.ReadinessChecks))

	r.Route("/api/v1", func(api chi.Router) {
		if opts.APIMounter != nil {
			opts.APIMounter(api)
		}
	})

	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		WriteProblem(w, req, http.StatusNotFound, "not-found", "Not found", "")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, req *http.Request) {
		WriteProblem(w, req, http.StatusMethodNotAllowed, "method-not-allowed", "Method not allowed", "")
	})

	return &http.Server{
		Addr:    opts.Config.HTTPAddr,
		Handler: r,
		// Timeouts close the Slowloris class of attacks: without them a
		// client holding a connection open trickle-feeding bytes pins a
		// goroutine and file descriptor indefinitely.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 * 1024,
	}
}
