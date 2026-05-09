# pkg/ — Shared Packages

Shared code imported by all services. Everything here is a Go library, not a standalone service.

## Subpackages

| Package | Purpose |
|---------|---------|
| `domain/` | Core models (User, Business, Integration), repository interfaces, sentinel errors |
| `a2a/` | Agent-to-Agent protocol: message types, base Agent, NATS transport |
| `llm/` | LLM Router, provider adapters (OpenRouter, OpenAI, Anthropic, SelfHosted), rate limiter, billing |
| `crypto/` | AES-256-GCM encryption for OAuth tokens |
| `logger/` | Structured logging wrapper (slog) |
| `health/` | Liveness/readiness probe helpers shared across services |
| `metrics/` | Prometheus counters/histograms (LLM, tool dispatch, HTTP middleware) |
| `tokenclient/` | HTTP client that fetches decrypted integration tokens from the API internal endpoint (used by platform agents) |
| `orchestratorclient/` | HTTP client for the orchestrator's cluster-internal endpoints (chat stream, HITL resume, tool registry, draft-reply) — consumed by `services/api`. See [`orchestratorclient/AGENTS.md`](orchestratorclient/AGENTS.md). |
| `agentbase/` | Shared `TokenResolver` / `Dispatcher` / `ErrorClassifier` / Redis-dedupe / env helpers consumed by all four platform agents (extracted from previous 4× duplications). See [`agentbase/AGENTS.md`](agentbase/AGENTS.md). |
| `hitldedupe/` | Redis-backed dedupe client for HITL approvals (consumed by `agentbase.Dispatcher`) |

## Rules

- **Only shared code goes here.** If it's used by one service, it belongs in that service's `internal/`.
- All models live in `domain/` — services import, never redefine.
- Repository interfaces live in `domain/repository.go` — implementations live in each service.
- Sentinel errors: `var ErrXxx = errors.New("...")` in `domain/errors.go`.

## Build & Test

```bash
cd pkg && go test -race ./...
cd pkg && golangci-lint run --config ../.golangci.yml ./...
```
