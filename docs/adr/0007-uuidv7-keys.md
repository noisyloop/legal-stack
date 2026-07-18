# ADR-0007: UUIDv7 primary keys

Status: Accepted · Date: 2026-07-18

## Context

Sequential bigint keys leak information (row counts, creation order across
tenants) and are guessable — bad properties for URLs in a legal platform.
Random UUIDv4 keys fix that but destroy B-tree insert locality (random page
writes, index bloat) and make keys useless for rough time-ordering.

## Decision

**UUIDv7** (time-ordered, RFC 9562) for all primary keys, generated
**client-side in the application** (`google/uuid`):

- Time-prefixed → near-sequential B-tree inserts, good locality.
- 74 random bits → unguessable identifiers.
- App-side generation → IDs exist before INSERT, so aggregates can be built,
  audit events composed, and idempotency keys derived without a DB roundtrip.
- The DB default (`gen_random_uuid()`) remains as a safety net for ad-hoc
  inserts.

The timestamp inside a v7 UUID reveals creation time. That is acceptable:
creation time is not sensitive for our resources, and is stored in
`created_at` anyway. Where even that leaks too much (none identified yet),
a v4 exception may be documented per-table.

## Consequences

- ✅ Unguessable, non-enumerable public identifiers; healthy indexes.
- ⚠️ IDs are 16 bytes vs 8 for bigint — negligible at our scale.
