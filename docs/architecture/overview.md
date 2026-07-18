# Architecture Overview

## 1. System context

Legal Stack is a multi-tenant SaaS platform. A **tenant is an Organization**
(a law firm, a management company, a label, or a client entity). Users belong
to one or more organizations through **memberships** carrying roles. Work
happens inside **Matters** — secure workspaces that own messages, documents,
tasks, audio assets, invoices, and an activity timeline.

```
┌────────────┐      HTTPS (TLS 1.3)      ┌─────────────────────────────┐
│  Browser    │ ─────────────────────────▶│  Edge (Cloudflare / NGINX)  │
│  Next.js UI │ ◀─────  WebSockets  ──────│  WAF, rate limit, TLS       │
└────────────┘                            └──────────────┬──────────────┘
                                                         │
                                          ┌──────────────▼──────────────┐
                                          │        Go API server        │
                                          │  (modular monolith, Chi)    │
                                          └──┬───────┬───────┬───────┬──┘
                                             │       │       │       │
                                     ┌───────▼──┐ ┌──▼───┐ ┌─▼────┐ ┌▼──────────┐
                                     │PostgreSQL│ │Redis │ │ S3   │ │ Workers    │
                                     │ (system  │ │cache/│ │object│ │ (jobs: AV  │
                                     │of record)│ │queues│ │store │ │ scan, etc) │
                                     └──────────┘ └──────┘ └──────┘ └───────────┘
```

## 2. Architectural style: modular monolith, hexagonal core

We deliberately start with a **modular monolith** (see ADR-0001), not
microservices. The codebase is organized as independent feature modules with
enforced boundaries; each module follows **Clean/Hexagonal Architecture**:

```
┌──────────────────────────────────────────────────────────┐
│ Transport (HTTP handlers, WebSocket, jobs)   ← adapters  │
│   depends on ↓                                           │
│ Application (use cases / command+query services)         │
│   depends on ↓                                           │
│ Domain (entities, value objects, invariants, ports)      │
│   ↑ implemented by                                       │
│ Infrastructure (PostgreSQL repos, Redis, S3, KMS, SMTP)  │
└──────────────────────────────────────────────────────────┘
```

**The dependency rule:** source code dependencies point *inward*. The domain
layer imports nothing from transport or infrastructure. Infrastructure
implements interfaces (**ports**) declared by the domain/application layers.
This is what makes the system testable (fakes for every port) and evolvable
(swap PostgreSQL full-text search for OpenSearch without touching use cases).

**Why not microservices on day one?** Distribution is the most expensive
architectural property to buy: it costs you transactions, type-checked
contracts, simple deployment, and easy local dev — and returns independent
scaling and team autonomy we do not yet need. The module boundaries we enforce
now *are* the future service boundaries; extraction later is mechanical.

## 3. Module map

Modules live under `backend/internal/<module>`. Each owns its tables, its
domain model, and its HTTP surface. Cross-module calls go through Go
interfaces (application services), never by reaching into another module's
repository or tables.

| Module | Responsibility |
|---|---|
| `platform` | Cross-cutting technical concerns: config, logging, DB pool, migrations, HTTP server, middleware, crypto helpers. Contains **no business logic**. |
| `audit` | Immutable, hash-chained audit trail. Every module records through its port. |
| `identity` | Organizations, users, memberships, invitations. |
| `auth` | Passkeys/WebAuthn, sessions, MFA, recovery codes, device management, lockout. |
| `authz` | RBAC role catalog + ABAC policy evaluation. One `Authorize(subject, action, resource)` entry point used by every handler. |
| `matter` | Matters, participants, timeline. |
| `document` | Files, versions, comments, approvals, retention, integrity hashes. |
| `storage` | Object storage port: multipart uploads, signed URLs, envelope encryption, virus-scan orchestration. |
| `messaging` | Channels, threads, messages, reactions, presence (WebSocket). |
| `task` | Tasks and assignments. |
| `calendar` | Events, deadlines, reminders. |
| `notification` | In-app + email notification fan-out. |
| `music` | Music-law domain: split sheets, ISRC/UPC, masters/stems metadata, royalties. |
| `ai` | Permission-aware RAG assistant. |
| `admin` | Platform administration surface. |

## 4. Request lifecycle (the Zero-Trust path)

Every API request passes through the same pipeline — there is no trusted
internal path that skips checks:

1. **Edge**: TLS termination, WAF rules, coarse rate limiting.
2. **Middleware**: request ID + correlation ID, real IP resolution, panic
   recovery, security headers, body size limits, per-principal rate limiting.
3. **Authentication**: session cookie → server-side session lookup → principal.
   No principal ⇒ 401. Sessions are opaque tokens, never JWTs (ADR-0004).
4. **Authorization**: handler declares `(action, resource)`; the `authz`
   engine evaluates RBAC role + ABAC attributes (org, matter participation,
   document ACL, time-boxed grants). Deny by default.
5. **Use case**: application service executes the command/query inside a DB
   transaction where needed.
6. **Audit**: the use case records an audit event **in the same transaction**
   as the state change — an action and its audit record commit or roll back
   together (ADR-0006).
7. **Response**: typed errors mapped to RFC 7807 problem+json; no internal
   details leak.

## 5. CQRS — where and why

We use **CQRS-lite**: commands and queries are separate application services
with separate models, but share one PostgreSQL database. Full event sourcing
is rejected (complexity, replay tooling, eventual-consistency UX costs) —
but the append-only audit log gives us the forensic history event sourcing
would provide. Read models (e.g., matter timeline, notification feed) are
denormalized tables maintained transactionally or by background jobs, chosen
per feature and documented in that feature's design.

## 6. Background work

Long-running work (virus scanning, metadata extraction, notification fan-out,
retention sweeps) runs on a worker pool consuming a PostgreSQL-backed job
queue (`FOR UPDATE SKIP LOCKED`), with Redis for scheduling hints and
rate limiting. PostgreSQL-backed jobs (not Redis lists) because jobs must be
transactional with the state that created them: "document row created +
scan job enqueued" is one transaction — no lost jobs, no phantom jobs.

## 7. Multi-tenancy & isolation

Shared database, shared schema, `org_id` on every tenant-owned row, enforced
in three layers (ADR-0003):

1. Application: every repository query is scoped by `org_id` from the request
   context — never from client input.
2. Database: PostgreSQL **Row-Level Security** policies as a backstop; the API
   role cannot see rows outside the org bound to the connection's session
   variable.
3. Audit: cross-org access attempts are logged and alertable.

## 8. Observability

- **Structured logs** (Go `slog`, JSON) with `request_id`, `correlation_id`,
  `org_id`, `user_id` on every line — but never tokens, passwords, or document
  content.
- **Traces & metrics** via OpenTelemetry → Prometheus/Grafana; logs → Loki.
- **Health**: `/healthz` (liveness — process is up) vs `/readyz` (readiness —
  dependencies reachable). Separated so an overloaded DB takes a pod out of
  rotation without triggering restarts.
