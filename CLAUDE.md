# OneVoice

Platform-agnostic multi-agent system for automating digital presence management.
Go 1.24 + Next.js 14 + PostgreSQL + NATS + Playwright RPA.

**Dev workflow is mandatory:** [WORKFLOW.md](WORKFLOW.md)

## Quick Reference

- **Architecture:** [docs/architecture.md](docs/architecture.md)
- **Golden Principles:** [docs/golden-principles.md](docs/golden-principles.md)
- **Code Patterns:** [docs/patterns.md](docs/patterns.md)
- **Anti-patterns:** [docs/anti-patterns.md](docs/anti-patterns.md)
- **Coding Rules (detailed):** [CODING_RULES.md](CODING_RULES.md)
- **Dev Workflow:** [WORKFLOW.md](WORKFLOW.md)

## Module Map

| Module | Path | Purpose | Port |
|--------|------|---------|------|
| Shared | `pkg/` | Domain models, auth, LLM router, A2A framework | — |
| API | `services/api/` | REST API, auth, business CRUD | 8080 |
| Orchestrator | `services/orchestrator/` | LLM agent loop, tool dispatch via NATS | 8090 |
| Frontend | `services/frontend/` | Next.js dashboard | 3000 |
| Telegram Agent | `services/agent-telegram/` | Telegram Bot API integration | — |
| VK Agent | `services/agent-vk/` | VK API integration | — |
| YaBiz Agent | `services/agent-yandex-business/` | Yandex.Business RPA via Playwright | — |

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
