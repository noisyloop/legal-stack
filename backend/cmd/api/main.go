// Command api is the composition root: the only place where concrete
// infrastructure (PostgreSQL, config, logging) is constructed and wired into
// the modules' ports. Everything below main receives interfaces.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-chi/chi/v5"

	"github.com/noisyloop/legal-stack/backend/internal/audit"
	"github.com/noisyloop/legal-stack/backend/internal/identity"
	"github.com/noisyloop/legal-stack/backend/internal/platform/config"
	"github.com/noisyloop/legal-stack/backend/internal/platform/database"
	"github.com/noisyloop/legal-stack/backend/internal/platform/httpserver"
	"github.com/noisyloop/legal-stack/backend/internal/platform/logging"
	"github.com/noisyloop/legal-stack/backend/migrations"
)

func main() {
	if err := run(); err != nil {
		// Config/boot failures happen before the structured logger may
		// exist; stderr is the reliable channel of last resort.
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := newLogger(cfg)
	slog.SetDefault(logger)
	logger.Info("starting legal-stack api", "env", string(cfg.Env), "addr", cfg.HTTPAddr)

	// Root context cancels on SIGINT/SIGTERM and drives graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	// Migrations run before the server binds: a replica must never serve
	// traffic against a schema it doesn't understand. The advisory lock in
	// the runner makes this safe under rolling deploys.
	if err := database.Migrate(ctx, pool, migrations.FS, logger); err != nil {
		return err
	}

	// Module wiring (grows as modules land).
	recorder := audit.NewRecorder()
	identityRepos := identity.NewPostgresRepositories(pool)
	_ = identity.NewService(pool, identityRepos, recorder) // HTTP surface arrives with authN (Phase 2)

	server := httpserver.New(httpserver.Options{
		Config:          cfg,
		Logger:          logger,
		ReadinessChecks: map[string]httpserver.Pinger{"database": pool},
		APIMounter: func(r chi.Router) {
			// Module routes mount here as their HTTP surfaces land.
		},
	})

	errCh := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}

	// Graceful drain: stop accepting, let in-flight requests finish within
	// the shutdown budget, then exit. Kubernetes/systemd send SIGTERM and
	// wait; this is the other half of that contract.
	logger.Info("shutting down", "timeout", cfg.ShutdownTimeout.String())
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	logger.Info("shutdown complete")
	return nil
}

func newLogger(cfg config.Config) *slog.Logger {
	format := cfg.LogFormat
	if !cfg.IsProduction() && os.Getenv("LOG_FORMAT") == "" {
		format = "text" // humans read dev logs; machines read prod logs
	}
	return logging.New(format, cfg.LogLevel)
}
