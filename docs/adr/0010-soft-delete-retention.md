# ADR-0010: Soft delete + retention policies (no hard deletes in the request path)

Status: Accepted · Date: 2026-07-18

## Context

Legal practice has conflicting obligations: preserve records (malpractice
defense, legal hold, discovery) and dispose of them (client agreements,
privacy law, data minimization). A `DELETE` statement in a request handler
cannot arbitrate that conflict.

## Decision

- **User-facing delete = soft delete**: `deleted_at` (+ `deleted_by`)
  timestamp; default queries exclude soft-deleted rows via views/repository
  scoping. The delete is audited like any mutation.
- **Hard deletion happens only via the retention engine**: per-org,
  per-resource-class policies (e.g., "purge client documents 7 years after
  matter close") executed by a background sweeper that itself writes audit
  events, and blocked by **legal hold** flags on matters.
- **Object storage follows the database**: blob deletion is driven by the
  sweeper; per-org KEK destruction (ADR-0008) provides provable
  crypto-shredding at org offboarding.
- Uniqueness with soft deletes uses partial indexes
  (`UNIQUE ... WHERE deleted_at IS NULL`) so a deleted slug/email can be
  reused without resurrecting the old row.

## Consequences

- ✅ Reversible mistakes, defensible disposal, legal-hold correctness.
- ⚠️ Every query must respect `deleted_at` — centralized in repositories and
  covered by tests, not left to caller memory.
- ⚠️ "Deleted" data still exists until retention purge — communicated
  honestly in the product UI ("moved to trash / pending disposal").
