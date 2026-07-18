package httpserver

import (
	"context"
	"net/http"
	"time"
)

// Pinger is the dependency-health port. The server package depends on this
// interface, not on pgxpool, so health checking is testable with fakes and
// new dependencies (Redis, S3) plug in without touching this file.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Liveness ("am I running?") must not check dependencies: if the DB is down,
// restarting the process won't help, and a liveness-driven restart storm
// makes the incident worse. Readiness ("should I receive traffic?") does
// check them, so load balancers drain unhealthy replicas instead.

func handleHealthz() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, r, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func handleReadyz(deps map[string]Pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		components := make(map[string]string, len(deps))
		ready := true
		for name, dep := range deps {
			if err := dep.Ping(ctx); err != nil {
				// The error itself stays in logs; the body reveals only
				// component up/down, which is safe for an internal endpoint.
				components[name] = "unavailable"
				ready = false
			} else {
				components[name] = "ok"
			}
		}

		status := http.StatusOK
		state := "ready"
		if !ready {
			status = http.StatusServiceUnavailable
			state = "not_ready"
		}
		WriteJSON(w, r, status, map[string]any{"status": state, "components": components})
	}
}
