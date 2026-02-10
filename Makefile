.PHONY: help build run test test-coverage lint fmt clean
.PHONY: migrate-up migrate-down migrate-create db-seed
.PHONY: docker-up docker-down docker-logs docker-clean

# Variables
BINARY_NAME=api
MAIN_PATH=./cmd/main.go
MIGRATION_PATH=./migrations/postgres
GOWORK=off

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
test: ## Run all tests
	@echo "Running tests..."
	@cd pkg && go test ./... -v
	@cd services/api && GOWORK=$(GOWORK) go test ./... -v

test-coverage: ## Run tests with coverage
	@echo "Running tests with coverage..."
	@cd services/api && GOWORK=$(GOWORK) go test ./... -coverprofile=../../coverage.out
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Lint
lint: ## Run linters
	@echo "Running linters..."
	@cd pkg && golangci-lint run ./...
	@cd services/api && golangci-lint run ./...

fmt: ## Format Go code
	@echo "Formatting code..."
	@cd pkg && go fmt ./...
	@cd services/api && go fmt ./...

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

# Docker
docker-up: ## Start all services with Docker Compose
	@echo "Starting services..."
	@docker-compose up -d
	@echo "Services started. API available at http://localhost:8080"

docker-down: ## Stop all services
	@echo "Stopping services..."
	@docker-compose down

docker-logs: ## View logs from all services
	@docker-compose logs -f

docker-clean: ## Remove volumes and clean up
	@echo "Cleaning up..."
	@docker-compose down -v
	@rm -rf data/

# Clean
clean: ## Remove build artifacts
	@echo "Cleaning..."
	@rm -rf bin/
	@rm -f coverage.out coverage.html
	@echo "Clean complete"
