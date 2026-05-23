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
                                ├── Yandex.Business Agent (Playwright RPA)
                                └── Google Business Agent (unverified — see services/agent-google-business/)
```

## Tech Stack

- **Backend:** Go 1.24, Chi router, SQLC, pgx
- **Frontend:** Next.js 14, TypeScript, Tailwind CSS, shadcn/ui
- **Messaging:** NATS (request/reply for tool dispatch)
- **Storage:** PostgreSQL 16, MongoDB 7, Redis 7, MinIO (S3)
- **LLM:** Multi-provider router (OpenRouter, OpenAI, Anthropic, self-hosted)
- **RPA:** Playwright (for platforms without public APIs)
- **Infra:** Docker Compose (single-VM); optional production overlay adds nginx + Let's Encrypt; Prometheus + Grafana observability

## Project Structure

```
pkg/                          # Shared Go packages (domain, auth, LLM router, A2A, health, metrics, tokenclient)
services/
  api/                        # REST API service (:8080)
  orchestrator/               # LLM agent loop, tool dispatch (:8090)
  frontend/                   # Next.js dashboard (:3000)
  agent-telegram/             # Telegram Bot API agent
  agent-vk/                   # VK API agent
  agent-yandex-business/      # Yandex.Business RPA agent (Playwright)
  agent-google-business/      # Google Business Profile agent (written, not yet verified)
migrations/                   # PostgreSQL + MongoDB migrations
nginx/                        # Reverse proxy: nginx.conf (dev), nginx.conf.template (prod, envsubst-rendered)
scripts/                      # Operational scripts (init-letsencrypt.sh, ...)
test/integration/             # End-to-end integration tests
docs/                         # Architecture, coding rules, deployment guide
```

## Quick Start (local dev)

### Prerequisites

- Docker and Docker Compose v2
- Go 1.24+
- Node.js 18+ and **pnpm**

### Run with Docker Compose

```bash
# Copy and configure environment (see .env.example for every required field)
cp .env.example .env

# Generate internal mTLS certs (one-shot)
make certs

# Start all services
docker compose up -d
```

Services will be available at:
- Frontend: http://localhost:3000
- API: http://localhost:8080
- Orchestrator: http://localhost:8090

### Local Development

```bash
# Install frontend dependencies
cd services/frontend && pnpm install && cd ../..

# Run Go services (requires infrastructure running via docker compose)
go run ./services/api/cmd
go run ./services/orchestrator/cmd
go run ./services/agent-telegram/cmd
go run ./services/agent-vk/cmd
go run ./services/agent-yandex-business/cmd
# go run ./services/agent-google-business/cmd  # written, not yet verified

# Run frontend
cd services/frontend && pnpm dev
```

## Deployment

Production-style single-VM deployment is documented end-to-end in [docs/deployment.md](docs/deployment.md). Three modes are supported:

| Mode | When to pick it | HTTPS? | OAuth (VK/Yandex)? | Custom domain? |
|---|---|---|---|---|
| **A — HTTP on bare IP** | Quick demo, Telegram only | No | No | No |
| **B — HTTPS via `<ip>.nip.io`** | Demo with full functionality, no domain to buy | Yes (real Let's Encrypt cert) | Yes | No |
| **C — HTTPS with your own domain** | Production / staging | Yes | Yes | Yes |

Modes B and C share the same `docker-compose.prod.yml` overlay (nginx 443 + certbot + healthchecks); only the `DOMAIN` value differs. Mode A uses the base `docker-compose.yml` unchanged.

Shortest path for a Yandex Cloud demo with HTTPS (~20 minutes):

```bash
cp .env.example .env       # fill DOMAIN=<ip-with-dashes>.nip.io, ACME_EMAIL=..., secrets
make certs
./scripts/init-letsencrypt.sh
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d
```

See [docs/deployment.md](docs/deployment.md) for prereqs (including Yandex Cloud security-group rules), secret generation, OAuth provider setup, smoke tests, operational tasks, rollback, and a troubleshooting table.

## Development Commands

```bash
make lint-all        # Go + frontend linting
make test-all        # Go + frontend tests
make fmt-fix         # Auto-format everything
```

## Documentation

- [Deployment guide](docs/deployment.md) — single-VM playbook, three modes, troubleshooting
- [Architecture](docs/architecture.md) — system diagrams + module map
- [Golden Principles](docs/golden-principles.md) — top-level rules enforced by linters
- Patterns / Anti-patterns: Go ([patterns](docs/go-patterns.md) · [anti](docs/go-antipatterns.md)) · Frontend ([patterns](docs/frontend-patterns.md) · [anti](docs/frontend-antipatterns.md))
- Rules by topic: [Go style](docs/go-style.md) · [Frontend style](docs/frontend-style.md) · [API design](docs/api-design.md) · [Security & perf](docs/security.md) · [Git workflow](docs/git-workflow.md)
- [CODING_RULES.md](CODING_RULES.md) — human-friendly index into the topic docs above

## License

[MIT](LICENSE) - Daniil Mikhailov, 2026
