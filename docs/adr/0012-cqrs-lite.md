# ADR-0012: CQRS-lite, no event sourcing

Status: Accepted · Date: 2026-07-18

## Context

Reads and writes have different shapes here: writes are invariant-heavy
commands ("approve document version"), reads are aggregation-heavy views
(matter timeline, notification feed, search). Full CQRS + event sourcing is
often paired with such systems, but event sourcing's costs (projection
rebuild tooling, eventual-consistency UX, schema evolution of events) are
severe, and our forensic-history requirement is already met by the
hash-chained audit log (ADR-0006).

## Decision

- **Separate command and query services** per module: commands go through
  domain aggregates enforcing invariants; queries may bypass the domain and
  read purpose-built projections or SQL views directly (they mutate nothing,
  so domain invariants aren't at risk — but they still pass authorization).
- **One PostgreSQL database**, current-state tables as the source of truth.
- Denormalized read models (e.g., `matter_timeline`) are maintained
  transactionally when staleness is unacceptable, or by background jobs when
  it is — decided and documented per feature.
- No event store; integration events (for notifications, search indexing)
  are rows in an outbox table published by a worker (transactional outbox
  pattern), giving at-least-once delivery without dual-write bugs.

## Consequences

- ✅ Invariants centralized on the write path; reads stay fast and simple.
- ✅ Transactional outbox gives reliable side effects without event sourcing.
- ⚠️ Two models per module is more code than CRUD — applied only where the
  shapes actually diverge; trivial modules may share a model until they hurt.
