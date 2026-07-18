# Database Design

PostgreSQL 17. Normalized (3NF) relational core; JSONB only for genuinely
schemaless payloads (audit old/new values, extracted file metadata).

## Conventions (every table)

- `id uuid PRIMARY KEY` — UUIDv7, app-generated (ADR-0007).
- `created_at` / `updated_at timestamptz NOT NULL` — always `timestamptz`
  (UTC); `updated_at` maintained by trigger so it can't be forgotten.
- Soft delete: `deleted_at timestamptz`, `deleted_by uuid` on user-facing
  resources (ADR-0010); uniqueness via partial indexes
  `WHERE deleted_at IS NULL`.
- Tenant-owned tables carry `org_id` FK `NOT NULL` (ADR-0003).
- FKs always declared; `ON DELETE RESTRICT` by default (deletion goes
  through the retention engine, not cascades). Every FK gets an index —
  PostgreSQL does not auto-index FK columns and unindexed FKs make both
  joins and parent-side checks sequential scans.
- Controlled vocabularies are `text` + `CHECK` constraints, not native
  enums: adding a value is a constraint swap, not a type migration, and
  values stay visible in `information_schema`.
- Names: `snake_case`, singular module prefix where ambiguous; migrations
  numbered `NNNN_description.sql`, forward-only (ADR-0011).

## Entity map (Phase 1 tables in **bold**)

```
**organizations** 1──* **org_memberships** *──1 **users**
      │                                            │
      │ 1──* matters 1──* matter_participants *────┘
      │         │
      │         ├──* documents 1──* document_versions ──* document_comments
      │         ├──* channels 1──* messages
      │         ├──* tasks
      │         └──* calendar_events
      │
      ├──* **audit_log**  (append-only, hash-chained)
      └──* permission_grants

**users** 1──* **webauthn_credentials**
      1──* **sessions**
      1──* **recovery_codes**
      1──* **auth_events**
```

## Phase 1 schema (implemented in `backend/migrations/`)

### organizations
Tenancy root. `kind` distinguishes `law_firm` / `client_org` / `management` /
`label` / `personal`. `slug` is `citext` (case-insensitive uniqueness) with a
partial unique index.

### users
Global account (a person exists once across orgs). `email citext` unique
among non-deleted rows. **No password column, by design** (ADR-0005).
`status`: `invited → active → suspended/deactivated`.

### org_memberships
The user×org join carrying `role` (RBAC anchor, see security model) and
`status`. Uniqueness `(org_id, user_id)` among live rows.

### webauthn_credentials
Passkey public keys: `credential_id` (bytea, unique), `public_key`,
`sign_count`, `transports`, `aaguid`, backup flags, `nickname`,
`last_used_at`, `revoked_at`. Server stores no secrets — only verifiers.

### sessions
`token_hash` (SHA-256 of the opaque cookie token — unique), device metadata
(`ip inet`, `user_agent`), `expires_at` (absolute), `idle_expires_at`,
`revoked_at` + `revoke_reason`. Partial index on live sessions per user for
the device-management screen.

### recovery_codes
`code_hash` (Argon2id), `used_at`. Single-use enforced by update guard.

### auth_events
Durable security telemetry (login success/failure, lockout, recovery use)
feeding lockout decisions and the user-visible security page. Distinct from
`audit_log`: high-volume, security-ops-shaped, per-user rather than
per-tenant-chain.

### audit_log (append-only)
```
seq bigint identity            -- global physical order
org_seq bigint                 -- per-org chain position
id uuid, occurred_at, org_id, matter_id?,
actor_user_id?, actor_type (user|system|service),
action text, resource_type text, resource_id?,
old_values jsonb?, new_values jsonb?, metadata jsonb,
ip inet?, user_agent?, request_id, correlation_id,
prev_hash bytea, hash bytea    -- SHA-256 chain (ADR-0006)
```
Trigger `audit_log_immutable` raises on UPDATE/DELETE. Indexed for the four
real query shapes: by org+time, by actor, by resource, by request ID.

## Future-phase tables (designed, not yet migrated)

- `matters`, `matter_participants` — Phase 4
- `documents`, `document_versions` (immutable rows; `sha256`, `size`,
  `wrapped_dek`, `storage_key`), `document_comments`, `document_approvals`,
  `retention_policies`, `legal_holds` — Phase 5
- `channels`, `messages` (soft-delete + edit history), `message_reactions`,
  `read_receipts` — Phase 6
- `jobs` (transactional queue, `FOR UPDATE SKIP LOCKED`), `outbox` — with
  first async consumer
- `split_sheets`, `recordings` (ISRC), `releases` (UPC), `works`,
  `royalty_schedules` — Phase 7
- `permission_grants`, `grant_approvals` — Phase 3

Each arrives with its feature's migration, design doc, and tests — schema
detail is decided at implementation time against these sketches.

## Operational rules

- Migrations run at API boot under an advisory lock (safe with replicas).
- `lock_timeout`/`statement_timeout` set in migrations so a bad migration
  fails fast instead of queueing behind traffic.
- No `SELECT *` in application SQL — column lists make schema changes
  reviewable and prevent accidental data exposure through new columns.
