# ADR-0004: Opaque server-side sessions, not JWTs, for first-party auth

Status: Accepted · Date: 2026-07-18

## Context

JWTs are the default answer for API auth, but for a first-party browser app
they are usually the wrong one: they cannot be revoked before expiry (a
stolen token stays valid), they push claims into a client-held artifact that
grows stale (role changes don't propagate), and their crypto agility
(`alg` confusion, key management) is a recurring vulnerability class.

Our security model requires: instant revocation (device management, account
lockout, "sign out everywhere"), server-visible session inventory, and
short-lived credentials for downloads.

## Decision

- **Browser sessions**: 256-bit random opaque token in an
  `httpOnly; Secure; SameSite=Lax` cookie (`__Host-` prefix). The server
  stores only **SHA-256(token)** — a database leak does not leak usable
  session tokens. Sessions carry ip/user-agent/device metadata, absolute and
  idle expiry, and a revocation flag; lookup is one indexed query.
- **CSRF**: SameSite=Lax + a double-submit token for state-changing requests
  (defense in depth; Lax alone doesn't cover all cross-site POST vectors on
  older browsers or subdomain compromise).
- **JWTs only where they fit**: short-lived (≤5 min), single-audience,
  asymmetric-signed tokens for delegated scenarios — signed download URLs,
  service-to-service calls after extraction, third-party API access. Never
  as the primary browser session.

## Consequences

- ✅ Instant revocation and full session inventory (device management UI).
- ✅ Stolen DB rows are hashes, not credentials.
- ⚠️ Every request costs a session lookup — mitigated with Redis read-through
  cache; revocation invalidates the cache entry synchronously.
- ⚠️ Horizontal scale needs shared session state — already true (PostgreSQL).
