# OneVoice

Platform-agnostic multi-agent system for automating digital presence management.

OneVoice provides a unified AI-powered interface for managing business presence across multiple platforms (Telegram, VK, Yandex.Business). It uses a hybrid integration model: API-based agents for platforms with public APIs, and RPA-based agents (Playwright) for platforms without.

## Architecture

```
Frontend (Next.js :3000)
    │
    ├── REST /api/v1/*  ──►  API Service (:8080)
    │                         ├── PostgreSQL (users, businesses, integrations)
    │                         ├── MongoDB (conversations, messages)
    │                         └── Redis (sessions, rate limits)
    │
    └── SSE /chat/*  ──►  Orchestrator (:8090)
                           ├── LLM Router (OpenRouter / OpenAI / Anthropic / SelfHosted)
                           └── Tool dispatch via NATS
                                ├── Telegram Agent (Bot API)
                                ├── VK Agent (VK API)
                                └── Yandex.Business Agent (Playwright RPA)
```

## Tech Stack

- **Backend:** Go 1.24, Chi router, SQLC, pgx
- **Frontend:** Next.js 14, TypeScript, Tailwind CSS, shadcn/ui
- **Messaging:** NATS (request/reply for tool dispatch)
- **Storage:** PostgreSQL 16, MongoDB 7, Redis 7, MinIO (S3)
- **LLM:** Multi-provider router (OpenRouter, OpenAI, Anthropic, self-hosted)
- **RPA:** Playwright (for platforms without public APIs)
- **Infra:** Docker Compose, Prometheus + Grafana observability

## Project Structure

```
pkg/                          # Shared Go packages (domain, auth, LLM router, A2A)
services/
  api/                        # REST API service (:8080)
  orchestrator/               # LLM agent loop, tool dispatch (:8090)
  frontend/                   # Next.js dashboard (:3000)
  agent-telegram/             # Telegram Bot API agent
  agent-vk/                   # VK API agent
  agent-yandex-business/      # Yandex.Business RPA agent
migrations/                   # PostgreSQL migrations
test/integration/             # End-to-end integration tests
docs/                         # Architecture and coding guidelines
```

## Quick Start

### Prerequisites

- Docker and Docker Compose
- Go 1.24+
- Node.js 18+

### Run with Docker Compose

```bash
# Copy and configure environment
cp .env.example .env

# Start all services
docker compose up -d
```

Services will be available at:
- Frontend: http://localhost:3000
- API: http://localhost:8080
- Orchestrator: http://localhost:8090

### Local Development

```bash
# Install dependencies
cd services/frontend && npm install && cd ../..

# Run Go services (requires infrastructure running via docker-compose)
go run services/api/cmd/main.go
go run services/orchestrator/cmd/main.go

# Run frontend
cd services/frontend && npm run dev
```

## Development Commands

```bash
make lint-all        # Go + frontend linting
make test-all        # Go + frontend tests
make fmt-fix         # Auto-format everything
```

## Documentation

- [Architecture](docs/architecture.md)
- [Coding Rules](CODING_RULES.md)
- [Code Patterns](docs/patterns.md)
- [Anti-patterns](docs/anti-patterns.md)
- [Golden Principles](docs/golden-principles.md)

## License

[MIT](LICENSE) - Daniil Mikhailov, 2026
