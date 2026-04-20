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

## Database Migrations

Two migration directories exist in this repo. **Both must carry the same logical schema.** When adding a table, column, or index for a phase, add a numbered file to BOTH paths using the next free slot in each.

| Path                       | Read by                                                                                     | Purpose                     |
|----------------------------|---------------------------------------------------------------------------------------------|-----------------------------|
| `migrations/postgres/`     | `docker-compose.yml` migrate service; `Makefile` `MIGRATION_PATH` (`make migrate-up`/`-down`/`-create`) | **Production / local dev.** |
| `services/api/migrations/` | `Makefile` integration-test target (`make integration-tests`)                                | **Integration tests only.** |

Numbering in each path is **independent**. Both start at `000001` with different contents; a migration numbered `000003` in one path is unrelated to `000003` in the other. When adding a migration, `ls` each directory first and use the next free slot in THAT path.

**Per-path idioms diverge — match them.** The two paths are not byte-identical. Each path has its own idioms you must follow when adding a migration:

- `services/api/migrations/000001_initial_schema.up.sql` loads the `uuid-ossp` extension and uses `uuid_generate_v4()` for UUID defaults. New migrations in this path MUST use `uuid_generate_v4()`.
- `migrations/postgres/000001_init.up.sql` uses the built-in `gen_random_uuid()` and does NOT load `uuid-ossp`. New migrations in this path MUST use `gen_random_uuid()`.

Logically the two copies define the same schema (same columns, same constraints, same indexes); only the path-specific idioms (UUID function, index names, etc.) differ.

Historical example: Phase 15 projects table is `000003_projects` in `services/api/migrations/` (uses `uuid_generate_v4()`) and `000004_projects` in `migrations/postgres/` (uses `gen_random_uuid()`) — `000003` in the prod path was already `add_user_token`. See `.planning/phases/15-projects-foundation/15-VERIFICATION.md` GAP-01 for the incident that surfaced this convention.
