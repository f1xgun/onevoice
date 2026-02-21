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

## Tool Names

All tools prefixed with `telegram__`:
- `telegram__send_channel_post`
- `telegram__get_channel_posts`
- `telegram__delete_channel_post`
- `telegram__get_chat_info`

## A2A Pattern

1. NATS subscription on `tasks.telegram`
2. Receive A2A TaskRequest with tool name + arguments
3. Dispatch to appropriate Telegram client method
4. Return A2A TaskResponse with result or error

## Build & Test

```bash
cd services/agent-telegram && GOWORK=off go test -race ./...
cd services/agent-telegram && golangci-lint run --config ../../.golangci.yml ./...
```
