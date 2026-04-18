# OneVoice

Platform-agnostic multi-agent system for automating digital presence management.
Go 1.24 + Next.js 14 + PostgreSQL + NATS + Playwright RPA.

## Where to Look

- **Architecture:** [docs/architecture.md](docs/architecture.md)
- **Golden Principles:** [docs/golden-principles.md](docs/golden-principles.md)
- **Patterns / Anti-patterns:** Go ([patterns](docs/go-patterns.md) · [anti](docs/go-antipatterns.md)) · Frontend ([patterns](docs/frontend-patterns.md) · [anti](docs/frontend-antipatterns.md))
- **Dev workflow:** managed via GSD slash commands (`/gsd:help`)
- **UI Reviewer:** [.claude/agents/ui-reviewer.md](.claude/agents/ui-reviewer.md)

## Rules by Topic

The agent should read only the topic docs relevant to the current task. Each links further into per-module `AGENTS.md` files.

| Scope | Rules |
|---|---|
| Go backend (`pkg/`, `services/api/`, `services/orchestrator/`, `services/agent-*/`) | [go-style.md](docs/go-style.md) · [go-patterns.md](docs/go-patterns.md) · [go-antipatterns.md](docs/go-antipatterns.md) |
| Frontend (`services/frontend/`) | [frontend-style.md](docs/frontend-style.md) · [frontend-patterns.md](docs/frontend-patterns.md) · [frontend-antipatterns.md](docs/frontend-antipatterns.md) |
| REST API endpoints (`services/api/`) | [docs/api-design.md](docs/api-design.md) |
| Secrets, auth, rate limiting, perf targets | [docs/security.md](docs/security.md) |
| Commits, branches, PRs | [docs/git-workflow.md](docs/git-workflow.md) |

Full human-readable index: [CODING_RULES.md](CODING_RULES.md).

## Module Map

Each module below has its own `AGENTS.md` with directory layout and scope-specific rules. Navigate into the relevant one instead of loading everything.

| Module | Path | Purpose | Port |
|--------|------|---------|------|
| Shared | [`pkg/`](pkg/AGENTS.md) | Domain, auth, LLM router, A2A, health, metrics, tokenclient | — |
| API | [`services/api/`](services/api/AGENTS.md) | REST API, auth, business CRUD | 8080 |
| Orchestrator | [`services/orchestrator/`](services/orchestrator/AGENTS.md) | LLM agent loop, tool dispatch via NATS | 8090 |
| Frontend | [`services/frontend/`](services/frontend/AGENTS.md) | Next.js dashboard | 3000 |
| Telegram Agent | [`services/agent-telegram/`](services/agent-telegram/AGENTS.md) | Telegram Bot API | — |
| VK Agent | [`services/agent-vk/`](services/agent-vk/AGENTS.md) | VK API | — |
| Yandex.Business Agent | [`services/agent-yandex-business/`](services/agent-yandex-business/AGENTS.md) | Playwright RPA (verified) | — |
| Google Business Agent | [`services/agent-google-business/`](services/agent-google-business/AGENTS.md) | Google Business Profile API (**unverified** — not yet live-tested) | — |

## Verification Commands

```bash
make lint-all        # Go + frontend linting
make test-all        # Go + frontend tests
make fmt-fix         # Auto-format everything
```

## Key Conventions

- Go workspace: `go.work` with all modules
- All Go modules: `replace github.com/f1xgun/onevoice/pkg => ../../pkg` in go.mod
- Tool naming: `{platform}__{action}` (e.g., `telegram__send_channel_post`)
- NATS subjects: `tasks.{agentID}` (e.g., `tasks.telegram`)
- Commit format: `<type>: <subject>` (feat, fix, refactor, docs, test, chore, ci)
