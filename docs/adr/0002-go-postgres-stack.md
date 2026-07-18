# ADR-0002: Go + Chi + PostgreSQL + Redis + S3-compatible storage

Status: Accepted · Date: 2026-07-18

## Context

We need a backend stack for a security-critical, long-lived system: strong
typing, small attack surface, first-class concurrency (WebSockets, workers),
boring operations.

## Decision

- **Go** for the API and workers: static binary, memory-safe, tiny container
  images (distroless), mature crypto in the standard library, `slog` for
  structured logging, race detector in CI.
- **Chi** as the router: idiomatic `net/http` (handlers are `http.Handler`),
  zero magic, middleware composition — unlike heavier frameworks, nothing
  hides the request path, which matters when auditing security behavior.
- **PostgreSQL** as the single system of record: RLS for tenant isolation,
  transactional DDL for safe migrations, JSONB for audit payloads, full-text
  search to start, `SKIP LOCKED` job queues.
- **pgx/v5** driver (no ORM): SQL is explicit and reviewable — in a legal
  platform, "what exactly does this query return" is a security question.
  Repositories encapsulate SQL behind ports, so query reuse is structured.
- **Redis** only for ephemeral state: rate-limit counters, WebSocket presence,
  cache. Never the system of record.
- **S3-compatible object storage** (MinIO locally) behind a storage port:
  keeps us portable across AWS S3 / R2 / GCS interop.

## Consequences

- ✅ Small dependency graph = small supply-chain surface (a stated goal).
- ✅ Everything testable against real PostgreSQL in CI (service container).
- ⚠️ No ORM means we write migrations and SQL by hand — accepted; it's also
  the point (learning + reviewability).
