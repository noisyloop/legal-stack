# API Conventions

REST, versioned at `/api/v1` (ADR-0009). OpenAPI 3.1 spec lives at
`docs/api/openapi.yaml` and grows with each endpoint; CI validates it.

## Requests

- JSON bodies (`application/json`), strict decoding: unknown fields
  rejected — silent acceptance hides client bugs and widens the parsing
  attack surface.
- Body size limits enforced by middleware (1 MiB default; uploads use the
  dedicated multipart flow, not JSON).
- **Idempotency**: mutating endpoints accept `Idempotency-Key`; the server
  stores the first response per (key, principal, route) for 24h and replays
  it on retry. Prevents double-billing/double-posting on network retries.
- Timestamps: RFC 3339 UTC in, RFC 3339 UTC out. IDs: UUID strings.

## Responses

- Errors: RFC 7807 `application/problem+json`:
  ```json
  { "type": "https://legal-stack.dev/errors/validation",
    "title": "Invalid request", "status": 422,
    "detail": "email is not a valid address",
    "request_id": "…" }
  ```
  `request_id` lets support correlate without leaking internals. 5xx bodies
  never contain error internals.
- Unauthorized reads of existing objects return **404** (anti-enumeration).
- Envelope for collections:
  ```json
  { "items": [...], "next_cursor": "opaque-or-null" }
  ```

## Pagination, filtering, sorting

- **Cursor pagination only** (`?cursor=…&limit=…`, limit capped at 100).
  Offset pagination is O(n) at depth, skips/duplicates rows under
  concurrent writes, and enables cheap DoS. Cursors are opaque
  (base64 of keyset values + HMAC so clients can't forge positions).
- Filtering via documented query params per endpoint (no generic query
  language). Sorting via `?sort=field,-other` from an allowlist.

## Versioning & compatibility

- Path versioning (`/api/v1`). Additive changes (new optional fields, new
  endpoints) don't bump the version; breaking changes do, with a
  deprecation window. `Sunset`/`Deprecation` headers announce removals.

## Rate limiting

- Layered: edge (coarse, per-IP) + application (per-principal, per-route
  class). `429` with `Retry-After`. Auth ceremonies have their own tighter
  budgets (see security model).

## Standard headers

| Header | Direction | Purpose |
|---|---|---|
| `X-Request-Id` | both | Server-generated per request; echoed for support |
| `X-Correlation-Id` | in/out | Client-provided chain across calls (validated format) |
| `Idempotency-Key` | in | Mutation replay safety |
| `Retry-After` | out | With 429/503 |
