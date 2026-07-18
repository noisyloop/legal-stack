.PHONY: help dev-infra dev-infra-down dev-full run-api build test test-integration lint sec-scan fmt frontend-dev

help: ## List targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-18s %s\n", $$1, $$2}'

dev-infra: ## Start local PostgreSQL, Redis, MinIO
	docker compose -f infra/compose/docker-compose.yml up -d --wait postgres redis minio

dev-infra-down: ## Stop local infrastructure (data volumes preserved)
	docker compose -f infra/compose/docker-compose.yml down

dev-full: ## Run everything in containers, including the API
	docker compose -f infra/compose/docker-compose.yml --profile full up --build

run-api: ## Run the API on the host against local infra
	cd backend && DATABASE_URL="postgres://app:devpassword@localhost:5432/legalstack?sslmode=disable" go run ./cmd/api

build: ## Build the API binary
	cd backend && go build -trimpath -o ../bin/api ./cmd/api

test: ## Backend unit tests (integration tests skip without TEST_DATABASE_URL)
	cd backend && go test -race ./...

test-integration: ## Full suite against local PostgreSQL (needs dev-infra)
	cd backend && TEST_DATABASE_URL="postgres://app:devpassword@localhost:5432/legalstack?sslmode=disable" go test -race ./...

lint: ## golangci-lint + go vet
	cd backend && go vet ./... && golangci-lint run ./...

sec-scan: ## Local security scans (govulncheck)
	cd backend && go run golang.org/x/vuln/cmd/govulncheck@latest ./...

fmt: ## Format Go code
	cd backend && gofmt -w . && go mod tidy

frontend-dev: ## Run the Next.js dev server
	cd frontend && npm run dev
