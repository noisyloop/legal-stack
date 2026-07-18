# Legal Stack — Secure Legal Collaboration Platform

An enterprise-grade, security-first collaboration platform for legal practice —
initially focused on music law, designed to support any practice area.

Think: **GitHub + Signal + Google Drive + Matter Management + Secure Client Portal.**

Every legal matter is a secure workspace where attorneys, clients, managers,
accountants, and outside counsel collaborate. Email is not the center of the
platform — secure, auditable, matter-scoped collaboration is.

> This repository is private and exists for educational purposes: learning
> enterprise architecture, security engineering, cryptography, distributed
> systems, cloud infrastructure, and DevSecOps by building a production-quality
> system end-to-end.

## Core philosophy

- **Security first, privacy first.** Zero Trust, least privilege, defense in depth.
- **Everything is auditable.** Immutable, tamper-evident audit trails (hash-chained).
- **Everything is versioned.** Nothing is permanently deleted outside retention policy.
- **Cryptographic integrity.** SHA-256 content hashes, envelope encryption, signed builds.

## Repository layout

```
backend/    Go API server — Clean Architecture / DDD modular monolith
frontend/   Next.js + TypeScript web application
infra/      Docker Compose dev stack, NGINX, Terraform (cloud IaC)
docs/       Architecture, ADRs, threat model, database & API design
.github/    CI/CD pipelines (lint, tests, security scans, SBOM)
```

Start with the docs — the system is designed before it is built:

| Document | Purpose |
|---|---|
| [docs/architecture/overview.md](docs/architecture/overview.md) | System architecture & module map |
| [docs/architecture/folder-structure.md](docs/architecture/folder-structure.md) | Code organization & layering rules |
| [docs/adr/](docs/adr/) | Architecture Decision Records (the *why*) |
| [docs/security/threat-model.md](docs/security/threat-model.md) | STRIDE threat model |
| [docs/security/security-model.md](docs/security/security-model.md) | AuthN, RBAC + ABAC, session design |
| [docs/database/schema.md](docs/database/schema.md) | Database design & conventions |
| [docs/api/conventions.md](docs/api/conventions.md) | REST API conventions & versioning |
| [docs/infrastructure/deployment.md](docs/infrastructure/deployment.md) | Environments, deployment, observability |
| [docs/roadmap.md](docs/roadmap.md) | Phased delivery plan |

## Quick start (local development)

Prerequisites: Go ≥ 1.24, Node ≥ 22, Docker + Compose.

```bash
# 1. Start infrastructure (PostgreSQL, Redis, MinIO)
make dev-infra

# 2. Run the API (applies migrations automatically)
make run-api

# 3. Verify
curl -s localhost:8080/healthz   # liveness
curl -s localhost:8080/readyz    # readiness (checks DB)
```

Run the test and lint suite:

```bash
make test        # unit tests
make test-integration  # requires dev infra running
make lint        # golangci-lint + vet
```

## Status

Phase 1 — Foundation. See [docs/roadmap.md](docs/roadmap.md) for the delivery plan.
