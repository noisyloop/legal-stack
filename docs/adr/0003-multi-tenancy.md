# ADR-0003: Shared-schema multi-tenancy with Row-Level Security backstop

Status: Accepted · Date: 2026-07-18

## Context

Tenant isolation is the platform's most important security property: a law
firm's matters leaking to another org is an extinction-level bug. Options:

1. **Database-per-tenant** — strongest isolation, but migrations × N,
   connection-pool explosion, cross-tenant features (a user in two orgs)
   become painful.
2. **Schema-per-tenant** — same operational pain, weaker win.
3. **Shared schema with `org_id` column** — operationally simple, but one
   missing `WHERE org_id = $1` is a data breach.

## Decision

Option 3, hardened by defense in depth:

1. **Application scoping**: `org_id` comes only from the authenticated
   request context, never from client input. Repository constructors take the
   tenant scope; queries without it don't compile past review.
2. **PostgreSQL RLS backstop**: tenant tables get
   `CREATE POLICY ... USING (org_id = current_setting('app.org_id')::uuid)`;
   the API's DB role is not `BYPASSRLS`. The request middleware sets
   `app.org_id` per transaction. A forgotten WHERE clause then returns zero
   rows instead of another tenant's data.
3. **Audit + alerting** on cross-tenant denials.

RLS policies are introduced per-table as tenant-owned tables land (Phase 3+),
because they must be designed together with the connection-scoping middleware.

## Consequences

- ✅ Two independent layers must both fail before data leaks.
- ✅ Single migration path, cheap tenants, cross-org users are natural.
- ⚠️ RLS adds per-query planning cost (~small; policies are single-column
  equality) and requires care with connection pooling (`SET LOCAL` inside
  the transaction, so scope can't leak across pooled connections).
- ⚠️ Backups are all-tenant; per-tenant export tooling needed for offboarding.
