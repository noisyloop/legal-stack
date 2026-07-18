# ADR-0009: REST + OpenAPI first (no GraphQL/gRPC on the public edge)

Status: Accepted · Date: 2026-07-18

## Context

The web app needs an API; later, integrations will too. GraphQL offers
client-shaped queries but complicates the security story (query-depth
attacks, per-field authorization, cache bypass) — per-field authZ in a
permission-heavy legal platform is exactly where GraphQL bites hardest.
gRPC is excellent service-to-service but hostile at the browser edge.

## Decision

- **Versioned REST** (`/api/v1/...`), resource-oriented, documented with an
  **OpenAPI 3.1** spec kept in-repo and validated in CI.
- RFC 7807 `application/problem+json` errors; cursor pagination; idempotency
  keys on mutating endpoints; explicit filtering/sorting parameters.
- Authorization is evaluated per-resource in one place (the authz engine) —
  natural in REST where the resource is the URL.
- gRPC reserved for future extracted services (module boundaries already
  interface-shaped); GraphQL revisited only if a strong aggregation need
  emerges (a BFF endpoint is the cheaper first answer).

## Consequences

- ✅ Simple to secure, cache, rate-limit, and audit (URL = resource = authZ
  object).
- ⚠️ Some over/under-fetching; mitigated with purpose-built read endpoints
  (CQRS-lite read models) rather than a generic query language.
