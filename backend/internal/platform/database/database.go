// Package database owns PostgreSQL connectivity and schema migrations.
package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect builds a pgx connection pool with production-safe defaults and
// verifies connectivity before returning — a process that cannot reach its
// database should fail its readiness gate immediately, not on first use.
func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("database: parse config: %w", err)
	}

	// Bounded pool: an unbounded pool converts load spikes into database
	// connection exhaustion, which takes out every replica at once.
	cfg.MaxConns = 20
	cfg.MinConns = 2
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 15 * time.Minute
	cfg.HealthCheckPeriod = 30 * time.Second

	// Server-side guardrails for every pooled connection. A runaway query
	// hits the statement timeout instead of holding locks indefinitely.
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "30000"        // 30s
	cfg.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = "60000" // 60s
	cfg.ConnConfig.RuntimeParams["application_name"] = "legal-stack-api"

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("database: create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database: ping: %w", err)
	}
	return pool, nil
}
