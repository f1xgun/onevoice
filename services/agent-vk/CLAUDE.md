# services/agent-vk/ — VK API Agent

Listens on NATS subject `tasks.vk`, executes VK API operations.

## Architecture

```
cmd/main.go              → wiring (NATS, VK client, A2A handler)
internal/
├── agent/handler.go     → A2A handler: dispatches tool calls to VK client
└── vk/client.go         → VK API client wrapper (vksdk/v3)
```

## Environment Variables

- `VK_ACCESS_TOKEN` (required) — VK API access token
- `NATS_URL` — NATS server (default: localhost:4222)

## Tools

Tool dispatch lives in `internal/agent/handler.go` (switch on `req.Tool`); tool *registration* with descriptions for the LLM is in `services/orchestrator/cmd/main.go` (search for `"vk__"`). Read both for the current tool set — do not trust enumerations in prose docs.

All tool names are prefixed with `vk__`.

## A2A Pattern

Same shape as all other platform agents:

1. NATS subscription on `tasks.vk`
2. Receive `a2a.ToolRequest` with `{Tool, BusinessID, Args}`
3. Dispatch via switch in `handler.go`
4. Return `a2a.ToolResponse` with result or error

## Build & Test

```bash
cd services/agent-vk && GOWORK=off go test -race ./...
cd services/agent-vk && golangci-lint run --config ../../.golangci.yml ./...
```
