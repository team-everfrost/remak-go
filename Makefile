.DEFAULT_GOAL := help

TOOLS_BIN := $(shell go env GOBIN)
ifeq ($(TOOLS_BIN),)
TOOLS_BIN := $(shell go env GOPATH)/bin
endif

.PHONY: help tools generate fmt fmt-check lint security test test-race test-integration test-legacy compose-up compose-down migrate legacy-migrate api worker check

help:
	@awk 'BEGIN {FS = ":.*##"; print "Usage: make <target>"} /^[a-zA-Z_-]+:.*##/ {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

tools: ## Install pinned development tools
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0
	go install github.com/pressly/goose/v3/cmd/goose@v3.26.0
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2

generate: ## Generate database access code
	$(TOOLS_BIN)/sqlc generate

fmt: ## Format source code
	$(TOOLS_BIN)/golangci-lint fmt

fmt-check: ## Fail if committed Go source is not formatted
	$(TOOLS_BIN)/golangci-lint fmt --diff

lint: ## Run static analysis
	go vet ./...
	$(TOOLS_BIN)/golangci-lint run

security: ## Check module integrity and reachable vulnerabilities
	go mod verify
	go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...

test: ## Run unit tests
	go test ./...

test-race: ## Run tests with race detector
	go test -race ./...

test-integration: ## Run integration tests against Docker services
	go test -tags=integration ./...

test-legacy: ## Rehearse legacy import in exact temporary databases
	./test/legacy-rehearsal.sh

compose-up: ## Start PostgreSQL and MiniStack
	docker compose up -d --wait postgres ministack

compose-down: ## Stop local dependencies
	docker compose down

migrate: ## Apply database migrations
	go run ./cmd/migrate up

legacy-migrate: ## Import a legacy database using DATABASE_URL and LEGACY_DB_* variables
	./db/legacy/run.sh

api: ## Run the API
	go run ./cmd/api

worker: ## Run the async worker
	go run ./cmd/worker

check: generate fmt-check lint test ## Run the local CI checks without rewriting source
