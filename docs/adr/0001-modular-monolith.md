# ADR-0001: Modular monolith over microservices

Status: Accepted · Date: 2026-07-18

## Context

The platform spans many modules (identity, auth, matters, documents,
messaging, music-law, AI). Microservices are the fashionable default for
"enterprise" systems, but they buy independent deployment/scaling at the cost
of distributed transactions, network failure modes, contract drift, and heavy
operational overhead. We are a small team; correctness and security matter
more than independent team velocity.

Critically, our hardest invariants are *transactional*: "state change +
audit record commit together", "document row + scan job enqueue together".
These are trivial in one database and genuinely hard across services.

## Decision

Build a single Go binary structured as a **modular monolith**: one process,
one database, but strictly bounded modules (`internal/<module>`) that
communicate only through Go interfaces wired at the composition root. Module
boundaries follow DDD bounded contexts and are treated as future service
boundaries.

## Consequences

- ✅ ACID transactions across "modules" where invariants demand it.
- ✅ One deployable, one local dev story, compile-time checked contracts.
- ✅ Extraction path preserved: a module with its own tables, its own domain
  model, and interface-only coupling can be lifted out behind gRPC later.
- ⚠️ Requires discipline (reviews + lint rules) to stop boundary erosion —
  the compiler will not stop a module importing another's repository.
- ⚠️ Whole-process scaling only, until hot modules (e.g., WebSocket fan-out)
  are split — acceptable at our scale; Go's footprint is small.
