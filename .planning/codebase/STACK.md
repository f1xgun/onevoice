# OneVoice Technology Stack

## Languages and Runtimes

- **Go:** 1.24.0 (workspace go.work at `go.work`)
  - Go packages: 1.23.0 minimum in `pkg/go.mod`
  - All services use Go 1.24.0 in respective go.mod files
  - Build target: Linux Alpine (CGO_ENABLED=0)

- **Node.js:** Runtime support for Next.js 14
  - Package manager: pnpm (lockfile implied by `pnpm.overrides` in `package.json`)
  - TypeScript: 5.x strict mode

## Go Workspace Structure

File: `go.work`

```
./pkg
./services/agent-telegram
./services/agent-vk
./services/agent-yandex-business
./services/api
./services/orchestrator
./test/integration
```

All services use `replace github.com/f1xgun/onevoice/pkg => ../../pkg` in their go.mod files.

## Backend Frameworks

- **chi/v5** (v5.2.5) — HTTP router
  - Used in: `services/api`, `services/orchestrator`
  - File: `services/api/go.mod`, `services/orchestrator/go.mod`

- **NATS** (nats.go v1.41.1) — Message broker
  - Used in: all services, agents, orchestrator
  - Subjects: `tasks.{agentID}` pattern
  - File: `docker-compose.yml` uses `nats:2.10-alpine`

## Frontend Stack

File: `services/frontend/package.json`

### Core Dependencies
- **Next.js:** 15.3.9 (App Router)
- **React:** 18.x
- **TypeScript:** 5.x
- **Tailwind CSS:** 3.4.1
  - **tailwind-merge:** 3.4.1
  - **tailwindcss-animate:** 1.0.7

### UI Component Library
- **Radix UI:** Full set of headless components
  - react-alert-dialog, react-avatar, react-dialog, react-dropdown-menu
  - react-label, react-popover, react-scroll-area, react-select
  - react-separator, react-slot, react-switch, react-tabs
  - react-toast, react-tooltip
  - All: 1.1.x - 2.1.x versions

### Form Management
- **react-hook-form:** 7.71.1
- **@hookform/resolvers:** 5.2.2
- **zod:** 4.3.6 (schema validation)

### State Management
- **Zustand:** 5.0.11 (global state)
- **TanStack React Query:** 5.90.21 (server state)

### HTTP Client
- **axios:** 1.13.5

### Utilities
- **lucide-react:** 0.564.0 (icons)
- **react-markdown:** 10.1.0 (markdown rendering)
- **sonner:** 2.0.7 (toast notifications)
- **date-fns:** 4.1.0 (date utilities)
- **clsx:** 2.1.1 (classname utilities)
- **class-variance-authority:** 0.7.1 (CSS-in-JS variants)

### Development Tools
- **ESLint:** 8.x
  - eslint-config-next: 15.3.9
  - eslint-config-prettier: 10.1.8
- **Prettier:** 3.8.1
  - prettier-plugin-tailwindcss: 0.7.2
- **Vitest:** 4.0.18 (test runner)
- **Testing Library:**
  - @testing-library/react: 16.3.2
  - @testing-library/jest-dom: 6.9.1
  - @testing-library/user-event: 14.6.1
- **jsdom:** 28.1.0 (DOM simulation)
- **PostCSS:** 8.x

## Databases

### PostgreSQL
- **Version:** 16-alpine (docker-compose.yml:4)
- **Go Driver:** github.com/jackc/pgx/v5 (v5.8.0)
- **Query Builder:** github.com/Masterminds/squirrel (v1.5.4)
- **Mocking:** github.com/pashagolub/pgxmock/v4 (v4.9.0)
- **Connection Pooling:** Built into pgx/v5
- **Location:** `services/api/internal/repository/` (SQL implementations)
- **Migrations:** `migrations/postgres/` (SQL migration files)
  - Tool: migrate/migrate:latest (docker-compose.yml:76)

### MongoDB
- **Version:** 7 (docker-compose.yml:24)
- **Go Driver:** go.mongodb.org/mongo-driver (v1.17.9 and v2.5.0)
- **Location:** `services/api/internal/repository/` (MongoDB implementations)
- **Initialization:** `migrations/mongo/` (MongoDB scripts)
- **Collections:** Conversations, Messages (from domain models)

### Redis
- **Version:** 7-alpine (docker-compose.yml:43)
- **Go Client:** github.com/redis/go-redis/v9 (v9.17.3)
- **Purpose:** Sessions, rate limiting, caching
- **Location:** Used in `services/api` and `services/orchestrator`
- **In-memory Testing:** github.com/alicebob/miniredis/v2 (v2.36.1)

## Message Broker

### NATS
- **Version:** 2.10-alpine with JetStream enabled (docker-compose.yml:59)
- **Configuration:**
  - JetStream enabled: `-js`
  - Data directory: `-sd /data`
  - Monitoring port: `-m 8222`
- **Go Client:** github.com/nats-io/nats.go (v1.41.1)
- **Transport Layer:** `pkg/a2a/nats_transport.go`
- **Subjects Pattern:** `tasks.{agentID}`
  - `tasks.telegram` — Telegram agent
  - `tasks.vk` — VK agent
  - `tasks.yandex_business` — Yandex.Business agent

## External API Integrations

### LLM Providers

File: `pkg/llm/provider.go`, `pkg/llm/router.go`

**Provider SDK Versions:**

- **OpenAI:** github.com/sashabaranov/go-openai (v1.41.2)
- **Anthropic:** github.com/anthropics/anthropic-sdk-go (v1.22.1)
- **OpenRouter:** HTTP-based (no native SDK, using standard http client)

**Routing Strategy:**
- LLM_MODEL env variable specifies provider/model (e.g., `openai/gpt-4o-mini`)
- Orchestrator tries providers in priority order
- Config: `services/orchestrator/internal/config/config.go`

### Platform APIs

**Telegram Bot API**
- SDK: github.com/go-telegram-bot-api/telegram-bot-api/v5 (v5.5.1)
- Location: `services/agent-telegram/`
- Authentication: TELEGRAM_BOT_TOKEN env variable
- Tool Naming: `telegram__*` (telegram__send_channel_post, telegram__send_channel_photo, etc.)

**VK API**
- SDK: github.com/SevereCloud/vksdk/v3 (v3.2.2)
- Location: `services/agent-vk/`
- Authentication: OAuth 2.0 (VK_CLIENT_ID, VK_CLIENT_SECRET)
- Tool Naming: `vk__*` (vk__create_wall_post, vk__get_wall_posts, etc.)

**Yandex.Business**
- Approach: Browser automation via Playwright RPA
- SDK: github.com/playwright-community/playwright-go (v0.4501.1)
- Location: `services/agent-yandex-business/`
- Authentication: YANDEX_COOKIES_JSON (session cookies)
- Tool Naming: `yandex_business__*` (yandex_business__get_reviews, etc.)

## Authentication & Security

### JWT
- **Library:** github.com/golang-jwt/jwt/v5 (v5.3.1)
- **Key Length:** Minimum 32 characters (enforced in `services/api/internal/config/config.go:83`)
- **Env Variable:** JWT_SECRET (required, validated at startup)
- **Location:** `services/api/internal/middleware/auth.go`

### Token Encryption
- **Algorithm:** AES-256-GCM
- **Implementation:** `pkg/crypto/crypto.go`
- **Key Length:** Exactly 32 bytes
- **Env Variable:** ENCRYPTION_KEY (required, validated at startup)
- **Use Case:** OAuth access/refresh tokens stored encrypted in PostgreSQL

### OAuth 2.0
- VK: Client ID/Secret flow with authorization code
- Yandex.Business: Client ID/Secret flow with authorization code
- Telegram: Bot token-based authentication
- State Management: Redis-backed state validation

## Build Tools and Configuration

### Go Build
- **Workspace:** GOWORK=off for Docker builds
- **CGO:** CGO_ENABLED=0 (no C dependencies)
- **GOOS:** linux
- **Build Tool:** make (Makefile at root)

Makefile targets (`Makefile`):
- `make lint-all` — Go + frontend linting
- `make test-all` — Go + frontend tests
- `make fmt-fix` — Auto-format everything
- `make docker-up` — Start all services
- `make certs` — Generate mTLS certificates

### Frontend Build
- **Build Tool:** Next.js (next build)
- **Linting:** ESLint + Prettier
- **Testing:** Vitest

### Docker
- **Base Images:**
  - Go: golang:1.23-alpine (builder stage)
  - Runtime: alpine:latest
  - Frontend: Node.js 18+ (from services/frontend/Dockerfile)
- **Multi-stage Builds:** Yes, for Go services
- **Compose Version:** 3.8+

Files:
- `Dockerfile` — API server
- `services/frontend/Dockerfile` — Frontend
- `docker-compose.yml` — All services orchestration
- `test/integration/docker-compose.test.yml` — Integration testing

### Linting
- **Go:** golangci-lint (config: `.golangci.yml`)
- **Frontend:** ESLint + Prettier

## Environment Variables

### API Service (`services/api`)

Core:
- `PORT` (default: 8080)
- `INTERNAL_PORT` (default: 8443, mTLS)

PostgreSQL:
- `POSTGRES_HOST` (default: localhost)
- `POSTGRES_PORT` (default: 5432)
- `POSTGRES_USER` (default: postgres)
- `POSTGRES_PASSWORD` (required)
- `POSTGRES_DB` (default: onevoice)

MongoDB:
- `MONGO_URI` (default: mongodb://localhost:27017)
- `MONGO_DB` (default: onevoice)

Redis:
- `REDIS_HOST` (default: localhost)
- `REDIS_PORT` (default: 6379)
- `REDIS_PASSWORD` (optional)

Encryption:
- `JWT_SECRET` (required, min 32 chars)
- `ENCRYPTION_KEY` (required, exactly 32 bytes)

OAuth:
- `VK_CLIENT_ID`, `VK_CLIENT_SECRET`, `VK_REDIRECT_URI`
- `YANDEX_CLIENT_ID`, `YANDEX_CLIENT_SECRET`, `YANDEX_REDIRECT_URI`
- `TELEGRAM_BOT_TOKEN`

Integration:
- `ORCHESTRATOR_URL` (default: http://localhost:8090)
- `NATS_URL` (optional, defaults to empty for disabled NATS)
- `REVIEW_SYNC_INTERVAL_MINUTES` (default: 30)

File Upload:
- `UPLOAD_DIR` (default: ./uploads)
- `PUBLIC_URL` (default: http://localhost:8080)

### Orchestrator Service (`services/orchestrator`)

Core:
- `PORT` (default: 8090)
- `LLM_MODEL` (required, e.g., openai/gpt-4o-mini)
- `LLM_TIER` (default: free)
- `MAX_ITERATIONS` (default: 10)
- `NATS_URL` (default: nats://localhost:4222)

LLM Provider Keys (at least one required):
- `OPENROUTER_API_KEY`
- `OPENAI_API_KEY`
- `ANTHROPIC_API_KEY`

Self-hosted Endpoints (indexed):
- `SELF_HOSTED_N_URL` (N = 0, 1, 2, ...)
- `SELF_HOSTED_N_MODEL`
- `SELF_HOSTED_N_API_KEY` (optional)

### Agent Services

Telegram (`services/agent-telegram`):
- `TELEGRAM_BOT_TOKEN` (required)
- `NATS_URL` (default: localhost:4222)
- `API_INTERNAL_URL` (default: http://api:8443)

VK (`services/agent-vk`):
- `VK_ACCESS_TOKEN` (required)
- `NATS_URL` (default: localhost:4222)
- `API_INTERNAL_URL` (default: http://api:8443)

Yandex.Business (`services/agent-yandex-business`):
- `YANDEX_COOKIES_JSON` (required, JSON array of cookies)
- `NATS_URL` (default: localhost:4222)
- `API_INTERNAL_URL` (default: http://api:8443)

### Frontend

Build Args:
- `API_URL` (default: http://api:8080)
- `ORCHESTRATOR_URL` (default: http://orchestrator:8090)

Runtime:
- `NODE_ENV` (production in docker-compose.yml)

## Validation & Testing

### Test Dependencies
- github.com/stretchr/testify (v1.11.1) — assertions and mocks
- Testing patterns: `t.Setenv()` for environment isolation

### Database Testing
- PostgreSQL: `pashagolub/pgxmock` for mocking
- MongoDB: Test containers pattern
- Redis: `miniredis` for in-memory Redis

### Integration Testing
- Docker Compose test stack: `test/integration/docker-compose.test.yml`
- Makefile: `make test-integration`

## Configuration Management

**Approach:** Environment variables with sensible defaults

- API Config: `services/api/internal/config/config.go`
- Orchestrator Config: `services/orchestrator/internal/config/config.go`
- Agent Configs: Embedded in agent service main files
- LLM Config: YAML-based provider/pricing config (type definitions in `pkg/llm/config.go`)

**Required vs Optional:**
- `JWT_SECRET` — required, validated at startup
- `ENCRYPTION_KEY` — required, validated at startup
- `LLM_MODEL` — required for orchestrator, validated at startup
- All other variables have defaults or are optional
