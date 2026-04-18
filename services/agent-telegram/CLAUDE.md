# services/agent-telegram/ — Telegram Bot API Agent

Listens on NATS subject `tasks.telegram`, executes Telegram Bot API operations.

## Architecture

```
cmd/main.go              → wiring (NATS, Telegram bot, A2A handler)
internal/
├── agent/handler.go     → A2A handler: dispatches tool calls to Telegram client
└── telegram/bot.go      → Telegram bot client wrapper (go-telegram-bot-api/v5)
```

## Environment Variables

- `TELEGRAM_BOT_TOKEN` (required) — Telegram Bot API token
- `NATS_URL` — NATS server (default: localhost:4222)

## Tools

Tool dispatch lives in `internal/agent/handler.go` (switch on `req.Tool`); tool *registration* with descriptions for the LLM is in `services/orchestrator/cmd/main.go` (search for `"telegram__"`). Read both for the current tool set — do not trust enumerations in prose docs, they drift.

All tool names are prefixed with `telegram__`.

## Channel ID Resolution Pattern

`getSender` returns `(Sender, resolvedExternalID, error)`. Handlers: if the LLM-supplied `channel_id` fails `strconv.ParseInt`, fall back to `resolvedExternalID` from the integration. This covers empty, business-name, and hallucinated channel IDs.

## A2A Pattern

1. NATS subscription on `tasks.telegram`
2. Receive `a2a.ToolRequest` with `{Tool, BusinessID, Args}`
3. Dispatch to Telegram client method via switch in `handler.go`
4. Return `a2a.ToolResponse` with result or error (errors auto-wrapped by base agent)

## Build & Test

```bash
cd services/agent-telegram && GOWORK=off go test -race ./...
cd services/agent-telegram && golangci-lint run --config ../../.golangci.yml ./...
```
