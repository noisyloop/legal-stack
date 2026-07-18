# Folder Structure & Layering Rules

## Monorepo layout

```
legal-stack/
├── backend/                 # Go API server & workers
│   ├── cmd/
│   │   └── api/             # main() — composition root; the ONLY place
│   │                        # where concrete infrastructure is wired up
│   ├── internal/            # private packages (Go enforces this)
│   │   ├── platform/        # technical foundation (no business logic)
│   │   │   ├── config/      # env-driven configuration, validated at boot
│   │   │   ├── logging/     # slog setup, redaction helpers
│   │   │   ├── database/    # pgx pool, migration runner, embedded SQL
│   │   │   └── httpserver/  # server, middleware, health, responses
│   │   ├── audit/           # audit domain + hash chain + PG repository
│   │   ├── identity/        # orgs, users, memberships
│   │   ├── auth/            # (Phase 2) passkeys, sessions
│   │   ├── authz/           # (Phase 3) RBAC/ABAC engine
│   │   └── ...              # one directory per module
│   └── migrations/          # numbered SQL migrations (embedded at build)
├── frontend/                # Next.js app (App Router, TypeScript)
│   └── src/
│       ├── app/             # routes/layouts
│       ├── components/      # shared UI
│       ├── features/        # feature modules mirroring backend modules
│       └── lib/             # API client, utilities
├── infra/
│   ├── compose/             # docker-compose dev stack + service config
│   ├── nginx/               # reverse-proxy config (prod-like local)
│   └── terraform/           # cloud IaC (later phase)
├── docs/                    # architecture, ADRs, security, db, api
└── .github/workflows/       # CI/CD
```

## Inside a backend module

Each module keeps the hexagonal layers as *files/packages within the module*,
not as global top-level layers. Feature cohesion beats layer cohesion: when
you change "documents", you change one directory.

```
internal/document/
├── domain.go        # entities, value objects, invariants, domain errors
├── ports.go         # interfaces the module needs (repos, storage, clock)
├── service.go       # application layer: use cases (commands/queries)
├── http.go          # transport: HTTP handlers + request/response DTOs
├── postgres.go      # infrastructure: repository implementation
└── *_test.go        # unit tests (fakes for ports) + integration tests
```

## Layering rules (enforced in review + lint)

1. `domain.go` imports **only** the standard library and shared kernel types
   (IDs, money, time). Never `net/http`, never `pgx`.
2. `service.go` depends on domain + ports. It orchestrates; it does not
   contain SQL or HTTP.
3. `http.go` translates HTTP ⇄ use cases. No business rules — if a handler
   contains an `if` about legal workflow, it's in the wrong layer.
4. `postgres.go` implements ports. SQL lives here and only here.
5. Modules talk to each other through application-service interfaces injected
   in `cmd/api`. **Never** import another module's `postgres.go` or query its
   tables.
6. `platform/*` may be imported by any module; it imports no module.

## Why this shape

- **`internal/` visibility**: Go forbids importing `internal/` from outside
  the backend — the compiler enforces our public surface is only `cmd/`.
- **Composition root in `main`**: constructors receive interfaces; only
  `cmd/api` knows concrete types. This is dependency injection without a
  framework — explicit, compile-time checked, trivially testable.
- **Feature modules mirror on the frontend**: a full-stack feature change
  touches `backend/internal/X` + `frontend/src/features/X`, keeping reviews
  and ownership coherent.
