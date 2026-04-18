.PHONY: help build run test test-all test-coverage test-integration
.PHONY: lint lint-frontend lint-all fmt fmt-fix docs-check
.PHONY: migrate-up migrate-down migrate-create db-seed
.PHONY: up down logs restart restart-service docker-up docker-down docker-logs docker-clean
.PHONY: clean certs

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

test-all: test test-frontend ## Run all tests (Go + frontend)

test-frontend: ## Run frontend tests
	@echo "Running frontend tests..."
	@cd services/frontend && pnpm test

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

lint-frontend: ## Run frontend linters (ESLint + Prettier)
	@echo "Running frontend linters..."
	@cd services/frontend && pnpm lint
	@cd services/frontend && pnpm exec prettier --check .
	@echo "Frontend lint clean"

lint-all: lint lint-frontend docs-check ## Run all linters (Go + frontend + docs)

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

# Clean
clean: ## Remove build artifacts
	@echo "Cleaning..."
	@rm -rf bin/
	@rm -f coverage.out coverage.html
	@echo "Clean complete"
