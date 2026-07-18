# ADR-0006: Transactional, hash-chained audit log

Status: Accepted · Date: 2026-07-18

## Context

"Everything is auditable" and "tamper-evident" are core requirements. Two
failure modes to design against:

1. **Missing events** — an action succeeds but its audit record is lost
   (fire-and-forget logging, crashed async writer).
2. **Silent modification** — an attacker (or insider with DB access) edits or
   deletes audit rows after the fact.

## Decision

- **Same-transaction writes**: use cases append the audit event in the same
  DB transaction as the state change. Commit is atomic: no state change
  without its audit record, no audit record for rolled-back changes.
- **Append-only enforcement**: a trigger raises on any `UPDATE`/`DELETE` of
  `audit_log`; the API's DB role has only `INSERT`/`SELECT`.
- **Hash chain**: each event stores
  `hash = SHA-256(prev_hash ‖ canonical(event))` where `canonical(event)` is
  a fixed-order serialization of the immutable fields. `prev_hash` links to
  the previous event in the same organization's chain (per-org chains avoid
  a global serialization bottleneck and let a tenant's chain be verified in
  isolation). A background verifier walks chains and alarms on breaks;
  periodic chain-head checkpoints are exported to object storage (WORM) so
  even a full-DB rewrite is detectable.
- **Structured envelope**: actor, org, matter, action, resource, old/new
  values (JSONB, redacted of secrets), IP, user agent, request/correlation
  IDs, monotonic per-org sequence.

## Consequences

- ✅ Tamper-*evident* (not tamper-proof — nothing inside one DB can be):
  any edit breaks the chain from that point forward.
- ✅ Forensics-grade completeness because audit is transactional.
- ⚠️ Per-org chain append requires serializing on the org's chain head; we
  take a per-org advisory lock at append time. Contention is bounded per
  tenant and irrelevant at per-org write rates.
- ⚠️ Old/new values must pass through redaction before storage — audit must
  never become the place secrets leak *into*.
