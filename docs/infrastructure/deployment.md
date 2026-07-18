# Infrastructure & Deployment

## Environments

| Env | Purpose | Infra |
|---|---|---|
| `dev` | Local development | Docker Compose (PostgreSQL, Redis, MinIO) |
| `ci` | Automated tests | GitHub Actions service containers |
| `staging` | Prod-shaped rehearsal | Cloud, Terraform-managed (Phase 9) |
| `prod` | Production | Cloud, Terraform-managed (Phase 9) |

Configuration is **environment variables only** (12-factor), parsed and
validated at boot by `platform/config` — the process refuses to start with
invalid config (fail fast beats failing on the first request). `.env.example`
documents every variable; real values never enter the repo.

## Local development

`infra/compose/docker-compose.yml` runs PostgreSQL 17, Redis 8, MinIO.
The API runs on the host (`make run-api`) for fast iteration, or in a
container (`make dev-full`) for prod-parity testing behind NGINX.

## Container build

Multi-stage Dockerfile:
1. `golang:1.24` builder — `CGO_ENABLED=0`, `-trimpath`, version stamped via
   `-ldflags -X`.
2. `gcr.io/distroless/static-debian12:nonroot` runtime — no shell, no
   package manager, non-root UID, read-only rootfs. The attack surface of
   the runtime image is the binary itself.

Images are scanned (Trivy) and SBOM'd (Syft) in CI; releases are signed
(cosign, keyless OIDC) so deployment can verify provenance — this is the
"signed builds" control.

## Deployment topology (target)

```
Cloudflare (DNS, CDN, WAF, DDoS)
   └─► NGINX ingress (TLS 1.3, rate limits, body caps)
         └─► api (N replicas, stateless — sessions in PG/Redis)
               ├─► PostgreSQL (primary + replica, PITR backups)
               ├─► Redis (ephemeral state only — loss is graceful)
               ├─► S3 object storage (versioned, encrypted, WORM audit bucket)
               └─► KMS/Vault (per-org KEKs)
         └─► workers (same image, worker entrypoint)
```

Stateless API replicas mean deploys are rolling and boring; the only
stateful components are managed services with tested backups. Readiness
gates (`/readyz`) hold traffic until migrations and dependency checks pass.

## Observability

- **Logs**: slog JSON → stdout → Loki. Mandatory fields: `ts`, `level`,
  `msg`, `request_id`, and where present `org_id`, `user_id`, `route`,
  `status`, `duration_ms`. Redaction is centralized; log lines never carry
  tokens or document content.
- **Metrics**: Prometheus — RED per route (rate, errors, duration
  histograms), DB pool saturation, job-queue depth/age, WebSocket counts,
  audit-chain verifier lag.
- **Traces**: OpenTelemetry SDK, W3C `traceparent` propagation; sampled
  head-based in prod, 100% in staging.
- **Alerts** (initial): readiness flapping, p99 latency, 5xx rate,
  job-queue age, **audit chain verification failure** (page immediately —
  that's a security event, not an ops event).

## CI/CD (implemented in `.github/workflows/`)

Pipeline on every PR: lint → unit tests → integration tests (real
PostgreSQL service container, race detector) → gosec → govulncheck →
gitleaks → SBOM → dependency review. Frontend: typecheck, lint, build.
Deploys (later): tag → build+sign+scan image → staging → manual promotion
to prod. No direct-to-prod path.

## Disaster recovery (targets)

- PostgreSQL PITR, RPO ≤ 5 min, RTO ≤ 1 h; restores rehearsed quarterly.
- Object storage: versioning + cross-region replication for the audit
  checkpoint bucket.
- Runbooks live in `docs/runbooks/` as incidents are drilled (Phase 9).
