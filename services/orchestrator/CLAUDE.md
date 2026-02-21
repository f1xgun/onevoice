# services/orchestrator/ — LLM Orchestrator Service

Receives chat requests, runs the LLM agent loop, dispatches tool calls to platform agents via NATS.

**Port:** 8090

## Architecture

```
cmd/main.go             → wiring (LLM providers, NATS, tool registry, chi router)
internal/
├── config/             → env config (LLM_MODEL required, PORT=8090, MAX_ITERATIONS=10, NATS_URL)
├── handler/chat.go     → POST /chat/{conversationID} → SSE stream
├── orchestrator/       → Run(ctx, messages) → <-chan Event (agent loop)
├── prompt/builder.go   → Build(BusinessContext, history) → system prompt + messages
├── tools/registry.go   → Tool registry ({platform}__action naming, Available(integrations))
└── natsexec/           → NATSExecutor: sends tool calls via NATS request/reply
```

## Key Concepts

- **Agent loop:** LLM call → if tool_call → execute via NATS → feed result back → repeat (max iterations guard)
- **SSE format:** `data: {"type":"text|tool_call|tool_result|done|error","content":"..."}\n\n`
- **Tool naming:** `{platform}__{action}` → extracts platform → NATS subject `tasks.{platform}`
- **LLM Router:** tries providers in order: OpenRouter → OpenAI → Anthropic → SelfHosted

## Environment Variables

- `LLM_MODEL` (required) — model name for the LLM router
- `PORT` — HTTP port (default: 8090)
- `MAX_ITERATIONS` — max agent loop iterations (default: 10)
- `NATS_URL` — NATS server URL (default: nats://localhost:4222)
- Provider keys: `OPENROUTER_API_KEY`, `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`

## Build & Test

```bash
cd services/orchestrator && GOWORK=off go test -race ./...
cd services/orchestrator && golangci-lint run --config ../../.golangci.yml ./...
```
