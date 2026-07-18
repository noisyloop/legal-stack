# Architecture Decision Records

ADRs capture decisions that are expensive to reverse, with the context and
tradeoffs at the time they were made. Format: Context → Decision →
Consequences. Statuses: Proposed, Accepted, Superseded.

| # | Title | Status |
|---|---|---|
| [0001](0001-modular-monolith.md) | Modular monolith over microservices | Accepted |
| [0002](0002-go-postgres-stack.md) | Go + Chi + PostgreSQL + Redis + S3 | Accepted |
| [0003](0003-multi-tenancy.md) | Shared-schema multi-tenancy with RLS backstop | Accepted |
| [0004](0004-sessions-not-jwt.md) | Opaque server-side sessions, not JWTs | Accepted |
| [0005](0005-passkeys-first-auth.md) | Passkeys-first authentication | Accepted |
| [0006](0006-audit-hash-chain.md) | Transactional, hash-chained audit log | Accepted |
| [0007](0007-uuidv7-keys.md) | UUIDv7 primary keys | Accepted |
| [0008](0008-envelope-encryption.md) | Envelope encryption with KMS abstraction | Accepted |
| [0009](0009-rest-openapi.md) | REST + OpenAPI before GraphQL/gRPC | Accepted |
| [0010](0010-soft-delete-retention.md) | Soft delete + retention policies | Accepted |
| [0011](0011-internal-migration-runner.md) | Minimal internal migration runner | Accepted |
| [0012](0012-cqrs-lite.md) | CQRS-lite, no event sourcing | Accepted |
