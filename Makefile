.PHONY: help build run test test-all test-frontend test-a11y test-coverage test-integration
.PHONY: lint lint-rbac lint-urls lint-frontend lint-migrations lint-all fmt fmt-fix docs-check check-llm-defaults
.PHONY: check-legal-versions check-legal-versions-parity
.PHONY: migrate-up migrate-down migrate-create db-seed verify-rbac-backfill
.PHONY: up down logs restart restart-service docker-up docker-down docker-logs docker-clean
.PHONY: clean certs mtls-certs mtls-check clean-certs
.PHONY: oapi-install oapi-gen oapi-check

# Variables
BINARY_NAME=api
MAIN_PATH=./cmd/main.go
MIGRATION_PATH=./migrations/postgres
GOWORK=off
GOLANGCI_CONFIG=$(CURDIR)/.golangci.yml

# All Go modules (relative paths)
GO_MODULES=pkg services/api services/orchestrator services/agent-telegram services/agent-vk services/agent-yandex-business

# Help target
help: ## Show this help message
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

# Build
build: ## Build the API server
	@echo "Building $(BINARY_NAME)..."
	@cd services/api && GOWORK=$(GOWORK) go build -o ../../bin/$(BINARY_NAME) $(MAIN_PATH)
	@echo "Build complete: bin/$(BINARY_NAME)"

# Run
run: ## Run the API server
	@echo "Starting $(BINARY_NAME)..."
	@cd services/api && GOWORK=$(GOWORK) go run $(MAIN_PATH)

# Test
test: ## Run all Go tests with race detector
	@echo "Running Go tests..."
	@for mod in $(GO_MODULES); do \
		echo "  Testing $$mod..."; \
		cd $$mod && GOWORK=$(GOWORK) go test -race ./... && cd - > /dev/null || exit 1; \
	done
	@echo "All Go tests passed"

test-all: test test-frontend test-a11y ## Run all tests (Go + frontend + axe a11y gate)

test-frontend: ## Run frontend tests
	@echo "Running frontend tests..."
	@cd services/frontend && pnpm test

# BLOCKING accessibility gate.
#
# Runs vitest only on the axe-core audit suite. The audit fails (non-zero
# exit) when any `critical` or `serious` violation is detected on the
# OPEN mobile drawer + chat list, the SidebarSearch dropdown, or the
# ProjectSection context menu. `moderate` and `minor` violations are
# logged-only — they do NOT fail the build.
#
# Reuses the same vitest run that `pnpm test` already executes (the
# axe spec is part of the broader suite); this dedicated target makes
# the a11y gate independently invokable for CI debugging and grep-clean
# verification (`grep -E 'a11y|axe' Makefile`).
test-a11y: ## Run the axe-core a11y gate (BLOCKING on critical+serious)
	@echo "Running axe-core a11y gate..."
	@cd services/frontend && pnpm test:a11y

test-coverage: ## Run tests with coverage
	@echo "Running tests with coverage..."
	@cd services/api && GOWORK=$(GOWORK) go test ./... -coverprofile=../../coverage.out
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

test-integration: ## Run integration tests with Docker
	@echo "Starting test environment..."
	@cd test/integration && docker-compose -f docker-compose.test.yml up -d
	@echo "Waiting for services to be healthy..."
	@sleep 15
	@echo "Running database migrations..."
	@docker run --rm -v $(PWD)/services/api/migrations:/migrations --network host \
		migrate/migrate:latest \
		-path=/migrations \
		-database "postgres://test:test@localhost:5433/onevoice_test?sslmode=disable" up
	@echo "Running integration tests..."
	@TEST_API_URL=http://localhost:8081 \
	 TEST_POSTGRES_URL=postgres://test:test@localhost:5433/onevoice_test \
	 TEST_MONGO_URL=mongodb://localhost:27018 \
	 TEST_REDIS_URL=localhost:6380 \
	 go test -v ./test/integration/... || (cd test/integration && docker-compose -f docker-compose.test.yml logs api-test && exit 1)
	@echo "Cleaning up test environment..."
	@cd test/integration && docker-compose -f docker-compose.test.yml down -v
	@echo "Integration tests complete"

# Lint
lint: ## Run Go linters on all modules
	@echo "Running Go linters..."
	@for mod in $(GO_MODULES); do \
		echo "  Linting $$mod..."; \
		cd $$mod && golangci-lint run --config $(GOLANGCI_CONFIG) ./... && cd - > /dev/null || exit 1; \
	done
	@echo "All Go modules lint clean"

lint-rbac: ## Run RBAC drift checker (custom Go analyzer at tools/lint-rbac)
	@echo "Building lint-rbac..."
	@cd tools/lint-rbac && GOWORK=off go build -o lint-rbac .
	@echo "Running RBAC drift check..."
	@./tools/lint-rbac/lint-rbac services/api/internal/router/router.go
	@echo "RBAC drift check passed"

lint-urls: ## Reject inline http(s):// URL string literals (custom analyzer)
	@echo "Building nourl analyzer..."
	@cd tools/nourl && go build -o /tmp/onevoice-nourl .
	@echo "Checking for inline URLs..."
	@for mod in $(GO_MODULES); do \
		echo "  Scanning $$mod..."; \
		cd $$mod && /tmp/onevoice-nourl ./... && cd - > /dev/null || exit 1; \
	done
	@echo "All Go modules clean of inline URLs"

lint-frontend: ## Run frontend linters (ESLint + Prettier)
	@echo "Running frontend linters..."
	@cd services/frontend && pnpm lint
	@cd services/frontend && pnpm exec prettier --check .
	@echo "Frontend lint clean"

lint-migrations: ## Verify migration parity (no duplicate versions, paired up/down files)
	@echo "Checking migration parity..."
	@./scripts/check-migrations-parity.sh

# Asserts pkg/legalconfig/versions.go and
# services/frontend/lib/legal/versions.ts carry IDENTICAL version strings
# for tos / privacy / pdn. Drift causes 409 version_mismatch loops or
# accepted-with-wrong-version audit rows. Wired into lint-all so a PR
# bumping the Go const without the TS const (or vice versa) fails CI.
check-legal-versions-parity: ## Verify pkg/legalconfig and frontend/lib/legal version constants match
	@echo "Checking legal version parity..."
	@bash scripts/check-legal-versions-parity.sh

# Alias for the parity target (operator-friendly short name referenced
# by docs/runbook-launch-readiness.md §6 and the .env.example launch-gate
# comment block).
check-legal-versions: check-legal-versions-parity ## Alias for check-legal-versions-parity

# LLM default-model lint guard. Asserts .env.example + docker-compose.yml
# carry the Anthropic Sonnet 4.6 / Haiku 4.5 defaults so a future PR cannot
# silently revert them.
check-llm-defaults: ## Verify LLM_MODEL / TITLER_MODEL / DRAFT_REPLY_MODEL defaults
	@echo "Checking LLM default models..."
	@bash scripts/check-llm-defaults.sh

lint-all: lint lint-rbac lint-urls lint-frontend lint-migrations check-legal-versions-parity check-llm-defaults docs-check lint-no-pprof ## Run all linters (Go + RBAC drift + URL check + frontend + migration parity + legal version parity + LLM defaults + docs)

docs-check: ## Fail if docs reference tool names absent from Go code
	@./scripts/check-doc-tool-drift.sh

# Format
fmt: ## Check Go formatting
	@echo "Checking Go formatting..."
	@for mod in $(GO_MODULES); do \
		cd $$mod && gofmt -l . && cd - > /dev/null; \
	done

fmt-fix: ## Auto-format everything (Go + frontend)
	@echo "Formatting Go code..."
	@for mod in $(GO_MODULES); do \
		cd $$mod && gofmt -w . && cd - > /dev/null; \
	done
	@echo "Formatting frontend code..."
	@cd services/frontend && pnpm exec prettier --write .
	@echo "All code formatted"

# Migrations
migrate-up: ## Run database migrations
	@echo "Running migrations..."
	@migrate -path $(MIGRATION_PATH) -database "postgres://postgres:postgres@localhost:5432/onevoice?sslmode=disable" up

migrate-down: ## Rollback migrations
	@echo "Rolling back migrations..."
	@migrate -path $(MIGRATION_PATH) -database "postgres://postgres:postgres@localhost:5432/onevoice?sslmode=disable" down 1

verify-rbac-backfill: ## v2.0 RBAC: assert backfill produced no orphans/duplicates/missing-owners (exit non-zero on any violation)
	@psql "postgres://postgres:postgres@localhost:5432/onevoice?sslmode=disable" \
	    -v ON_ERROR_STOP=1 \
	    -f scripts/verify-rbac-backfill.sql

migrate-create: ## Create new migration (usage: make migrate-create name=add_users_table)
	@echo "Creating migration: $(name)"
	@migrate create -ext sql -dir $(MIGRATION_PATH) -seq $(name)

db-seed: ## Seed database with test data
	@echo "Seeding database..."
	@cd scripts && go run seed.go

# Docker — shortcuts
up: ## Start all services (build + migrate + run)
	@echo "Starting all services..."
	@docker compose up -d --build
	@echo ""
	@echo "Services started:"
	@echo "  Frontend:     http://localhost:80"
	@echo "  API:          http://localhost:8080"
	@echo "  Orchestrator: http://localhost:8090"
	@echo "  NATS monitor: http://localhost:8222"

down: ## Stop all services
	@docker compose down

restart: ## Rebuild and restart all services after code changes
	@echo "Rebuilding and restarting all services..."
	@docker compose down
	@docker compose up -d --build
	@echo ""
	@echo "Services restarted:"
	@echo "  Frontend:     http://localhost:80"
	@echo "  API:          http://localhost:8080"
	@echo "  Orchestrator: http://localhost:8090"
	@echo "  NATS monitor: http://localhost:8222"

restart-service: ## Rebuild and restart a single service (usage: make restart-service s=api)
	@echo "Rebuilding and restarting $(s)..."
	@docker compose up -d --build --no-deps $(s)
	@echo "$(s) restarted"

logs: ## Tail logs from all services
	@docker compose logs -f

# Docker — long-form aliases
docker-up: up ## Alias for 'up'

docker-down: down ## Alias for 'down'

docker-logs: logs ## Alias for 'logs'

docker-clean: ## Remove volumes and clean up
	@echo "Cleaning up..."
	@docker compose down -v
	@rm -rf data/

# Certificates
certs: ## Generate mTLS certificates for internal communication
	@echo "Generating certificates..."
	@mkdir -p certs
	@# CA
	@openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 \
		-days 3650 -nodes -keyout certs/ca.key -out certs/ca.crt \
		-subj "/CN=OneVoice Internal CA" 2>/dev/null
	@# Server (API internal)
	@openssl req -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 \
		-nodes -keyout certs/server.key -out certs/server.csr \
		-subj "/CN=api" 2>/dev/null
	@echo "subjectAltName=DNS:api,DNS:localhost,IP:127.0.0.1" > certs/server.ext
	@openssl x509 -req -in certs/server.csr -CA certs/ca.crt -CAkey certs/ca.key \
		-CAcreateserial -out certs/server.crt -days 3650 -extfile certs/server.ext 2>/dev/null
	@rm certs/server.csr certs/server.ext
	@# Agent clients
	@for agent in telegram vk yandex-business; do \
		openssl req -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 \
			-nodes -keyout certs/$$agent.key -out certs/$$agent.csr \
			-subj "/CN=agent-$$agent" 2>/dev/null; \
		openssl x509 -req -in certs/$$agent.csr -CA certs/ca.crt -CAkey certs/ca.key \
			-CAcreateserial -out certs/$$agent.crt -days 3650 2>/dev/null; \
		rm certs/$$agent.csr; \
	done
	@rm -f certs/ca.srl
	@echo "Certificates generated in certs/"

# mTLS certs: dev CA + per-service leaf certs that the API internal :8443
# listener and tokenclient mTLS use. Idempotent: running
# again with an existing infra/mtls/certs/ca.crt is a no-op.
mtls-certs: ## Generate dev CA + per-service leaf certs into infra/mtls/certs/ (idempotent)
	@bash scripts/gen-mtls-certs.sh

mtls-check: ## Verify every leaf cert against the dev CA + 30-day expiry window
	@bash scripts/check-mtls-certs.sh

clean-certs: ## Remove all generated mTLS material (next mtls-certs rebuilds)
	@find infra/mtls/certs -mindepth 1 ! -name '.gitignore' -delete 2>/dev/null || true
	@echo "infra/mtls/certs/ cleaned (kept .gitignore)"

# Clean
clean: ## Remove build artifacts
	@echo "Cleaning..."
	@rm -rf bin/
	@rm -f coverage.out coverage.html
	@echo "Clean complete"

# OpenAPI / oapi-codegen — types-only spec-first POC.
#
# Source of truth: docs/api/spec/openapi.yaml (modular; pulls in
# spec/paths/*.yaml + spec/components.yaml via external $refs).
# Generated output: services/api/internal/openapi/types.gen.go
# Config: docs/api/oapi-codegen.yaml
#
# Pipeline:
#   openapi.yaml --[tools/oapi-validate-tags]--> .openapi.validate.yaml --[oapi-codegen]--> types.gen.go
#
# The preprocessor walks the spec and injects
# `x-oapi-codegen-extra-tags: { validate: "..." }` annotations onto every
# property so the generated structs carry go-playground/validator.v10
# tags derived from JSON Schema constraints (format/min*/max*/pattern/
# enum/required). The intermediate spec is a build artifact — see the
# `.openapi.validate.yaml` entry in .gitignore.
OAPI_VERSION ?= v2.4.1
OAPI_BIN     ?= $(shell go env GOPATH)/bin/oapi-codegen
OAPI_SPEC    := docs/api/spec/openapi.yaml
OAPI_CONFIG  := docs/api/oapi-codegen.yaml
OAPI_OUT     := services/api/internal/openapi/types.gen.go
OAPI_PREP    := docs/api/spec/.openapi.validate.yaml

oapi-install: ## Install oapi-codegen CLI into $$GOPATH/bin (build-time tool, not a runtime dep)
	@echo "Installing oapi-codegen $(OAPI_VERSION)..."
	@GOWORK=off go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@$(OAPI_VERSION)
	@echo "Installed to $(OAPI_BIN)"

oapi-gen: ## Regenerate types from docs/api/spec/openapi.yaml
	@command -v $(OAPI_BIN) >/dev/null 2>&1 || { echo "oapi-codegen not found; run 'make oapi-install'"; exit 1; }
	@echo "Preprocessing $(OAPI_SPEC) -> $(OAPI_PREP) (inject validate tags)..."
	@go run ./tools/oapi-validate-tags $(OAPI_SPEC) $(OAPI_PREP)
	@echo "Generating $(OAPI_OUT) from $(OAPI_PREP)..."
	@$(OAPI_BIN) -config $(OAPI_CONFIG) $(OAPI_PREP)
	@rm -f $(OAPI_PREP)
	@echo "Generated $(OAPI_OUT)"

oapi-check: ## Fail if generated types are out of date relative to the spec
	@command -v $(OAPI_BIN) >/dev/null 2>&1 || { echo "oapi-codegen not found; run 'make oapi-install'"; exit 1; }
	@backup=$$(mktemp); \
		cp $(OAPI_OUT) $$backup; \
		go run ./tools/oapi-validate-tags $(OAPI_SPEC) $(OAPI_PREP); \
		$(OAPI_BIN) -config $(OAPI_CONFIG) $(OAPI_PREP); \
		rm -f $(OAPI_PREP); \
		if ! diff -u $$backup $(OAPI_OUT) >/dev/null; then \
			echo "drift detected: $(OAPI_OUT) does not match $(OAPI_SPEC)"; \
			diff -u $$backup $(OAPI_OUT) || true; \
			cp $$backup $(OAPI_OUT); \
			rm -f $$backup; \
			exit 1; \
		fi; \
		rm -f $$backup; \
		echo "$(OAPI_OUT) is up to date with $(OAPI_SPEC)"

.PHONY: lint-no-pprof docker-test-ulimit

lint-no-pprof: ## Reject net/http/pprof imports in prod build (SEC-17 CI gate)
	@! grep -rn 'net/http/pprof' services/ pkg/ --include='*.go' || (echo "pprof reintroduced — REJECT"; exit 1)

docker-test-ulimit: ## Verify ulimit -c 0 is set inside the container (supply ULIMIT_IMAGE=<image>)
	@docker run --rm $(ULIMIT_IMAGE) sh -c 'ulimit -c' | grep -qx '0' || (echo "ulimit -c should be 0 inside container"; exit 1)
