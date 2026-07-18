# ADR-0011: Minimal internal migration runner (embedded SQL)

Status: Accepted · Date: 2026-07-18

## Context

We need ordered, verifiable schema migrations. Off-the-shelf options
(golang-migrate, goose) are solid but bring wide dependency trees (drivers
for a dozen databases) into a codebase whose stated goal is a minimal
supply-chain surface; and their behavior (dirty-state handling, out-of-order
rules) is configuration we'd have to audit anyway.

Our needs are narrow: PostgreSQL only, forward-only, embedded in the binary,
transactional, checksummed.

## Decision

A ~200-line internal runner (`platform/database`):

- Migrations are numbered `NNNN_name.sql` files, **embedded** via `go:embed`
  — the binary is self-contained; the deployed artifact and its schema
  can't drift apart.
- A `schema_migrations` table records version, name, SHA-256 checksum, and
  apply time. On boot: verify checksums of already-applied files (detect
  history rewrites), then apply pending ones **each in its own transaction**
  (PostgreSQL has transactional DDL), under an advisory lock so concurrent
  replicas don't race.
- **Forward-only**: down-migrations give false confidence (most destructive
  changes don't reverse); recovery is roll-forward or restore-from-backup,
  which is what production incidents actually use.

## Consequences

- ✅ Zero new dependencies; behavior fully understood and unit-tested.
- ✅ Checksum verification makes migration history tamper-evident, matching
  the platform's integrity posture.
- ⚠️ We own edge cases (locking, partial failure). Accepted: the runner is
  small, tested, and PostgreSQL-only by design.
- ⚠️ No down-migrations by policy — documented in the runbook.
