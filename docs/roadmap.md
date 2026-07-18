# Delivery Roadmap

The platform is built incrementally. Each phase produces production-quality,
tested, documented code — no throwaway prototypes. Later phases never require
rewriting earlier phases because the architecture (module boundaries, schema
conventions, audit pipeline) is designed up front.

## Phase 0 — Design (complete)

- Architecture overview, layering rules, folder structure
- ADRs for every load-bearing decision
- STRIDE threat model & security model (authN/authZ)
- Database design & conventions
- API conventions
- Infrastructure & deployment design

## Phase 1 — Foundation (this phase)

- Monorepo scaffold (backend / frontend / infra / docs / CI)
- Go API skeleton: config, structured logging, hardened HTTP server,
  liveness/readiness, graceful shutdown
- PostgreSQL migrations engine + initial schema:
  organizations, users, memberships, WebAuthn credentials, sessions,
  recovery codes, auth events, **tamper-evident audit log**
- Audit hash-chain implementation + verification
- Docker Compose dev stack (PostgreSQL, Redis, MinIO)
- CI: lint, tests (with real PostgreSQL), gosec, govulncheck, gitleaks,
  SBOM generation, dependency review
- Next.js frontend scaffold

## Phase 2 — Authentication & Sessions

- Passkey (WebAuthn) registration + login ceremonies
- Opaque server-side sessions, httpOnly cookies, CSRF protection
- Recovery codes, account lockout, device management UI
- Rate limiting backed by Redis; IP intelligence hooks

## Phase 3 — Organizations, Users & Permissions

- Org onboarding, invitations, membership lifecycle
- RBAC role catalog + ABAC policy engine (matter/document scoping,
  time-limited grants, approval workflow)
- Admin surface

## Phase 4 — Matters

- Matter workspaces, participants, activity timeline
- Matter-scoped permission checks on every request

## Phase 5 — Documents & Object Storage

- Multipart upload, signed URLs, SHA-256 integrity, envelope encryption
- Virus-scanning pipeline, metadata extraction, version history
- Retention policies, legal hold

## Phase 6 — Messaging

- Channels, threads, WebSockets, presence, read receipts
- Encrypted message storage architecture

## Phase 7 — Music-law domain

- Split sheets, ISRC/UPC metadata, masters/stems, audio streaming previews
- Publishing/recording agreement templates, royalty structures

## Phase 8 — AI Assistant

- Permission-aware RAG (retrieval filtered by the same authZ engine),
  per-org isolation, summarization, clause search

## Phase 9 — Compliance & Scale

- SOC 2-style control mapping, DR runbooks, load testing, Terraform cloud IaC
