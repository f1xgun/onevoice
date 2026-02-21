# services/api/ — REST API Service

Main backend API. Handles auth, business management, integrations, conversations.

**Port:** 8080

## Architecture

```
cmd/main.go          → wiring (DB pools, Redis, repos, services, router)
internal/
├── config/          → env-based config (PostgreSQL, MongoDB, Redis, JWT)
├── handler/         → HTTP handlers (chi), request parsing, response formatting
├── middleware/      → auth (JWT), CORS, logging, rate limiting (Redis)
├── service/         → business logic, orchestrates repositories
├── repository/      → data access (pgx/squirrel for PG, mongo-driver for Mongo)
└── router/          → chi router setup, middleware chain
```

## Layer Rules

**Handler → Service → Repository** — never skip layers.

- Handlers: parse HTTP, call service, format response. No DB access.
- Services: business logic, validation, error wrapping. No HTTP concepts.
- Repositories: SQL/Mongo queries only. Convert DB errors to domain errors.

## Key Dependencies

- `chi/v5` — HTTP router
- `pgx/v5` — PostgreSQL (connection pool)
- `squirrel` — SQL query builder (prevents injection)
- `mongo-driver` — MongoDB for conversations/messages
- `go-redis/v9` — Redis for sessions and rate limiting
- `jwt/v5` — JWT authentication
- `validator/v10` — Request validation

## Build & Test

```bash
cd services/api && GOWORK=off go test -race ./...
cd services/api && golangci-lint run --config ../../.golangci.yml ./...
```
