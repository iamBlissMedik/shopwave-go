# Developer shortcuts. Every target is a thin wrapper around a plain `go`
# command, so nothing here is required — read a target to see what it runs.

TEST_DATABASE_URL ?= postgres://postgres:postgres@localhost:5432/shopwave_go_test?sslmode=disable

.PHONY: help run build test test-unit lint fmt tidy migrate-up migrate-down migrate-status seed db-up db-down

help: ## List targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-16s %s\n", $$1, $$2}'

run: ## Run the API (reads .env)
	go run ./cmd/api

build: ## Build binaries into ./bin
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/api ./cmd/api
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/migrate ./cmd/migrate

test: ## Unit + integration tests (needs Postgres; TEST_DATABASE_URL is truncated!)
	TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test -race ./...

test-unit: ## Unit tests only (no database)
	go test ./...

lint: ## gofmt check + go vet
	@test -z "$$(gofmt -l .)" || (echo "unformatted files:"; gofmt -l .; exit 1)
	go vet ./...

fmt: ## Format all code
	gofmt -w .

tidy: ## Sync go.mod / go.sum
	go mod tidy

migrate-up: ## Apply pending migrations
	go run ./cmd/migrate up

migrate-down: ## Roll back the latest migration
	go run ./cmd/migrate down

migrate-status: ## Show migration status
	go run ./cmd/migrate status

seed: ## WIPE the database and load demo data
	go run ./cmd/seed

db-up: ## Start a local Postgres in Docker
	docker compose up -d db

db-down: ## Stop the local Postgres
	docker compose down
