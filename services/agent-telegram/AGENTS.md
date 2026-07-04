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

`getSender` returns `(Sender, resolvedExternalID, error)`, where `resolvedExternalID` is **always one of the acting business's own integration external_ids** (the exact match when the supplied id names one of that business's integrations, else the first-active own channel — the token resolver's fallback). Handlers resolve the chat target via `resolveChatTarget(supplied, resolved)`.

**Ownership rule (cross-tenant guard):** one shared OneVoice system bot administers every tenant's connected channel, so an unverified LLM-supplied `channel_id` must never be trusted as the send target — a hallucinated or prompt-injected value naming a *different* tenant's channel would let the bot post the acting tenant's content onto a channel it does not own. `resolveChatTarget` therefore honors `supplied` **only when it exactly equals the business-verified `resolved`** value; every other supplied value (empty, business-name, hallucinated, or a foreign channel) falls back to the owned `resolved` channel. This preserves legitimate multi-channel targeting by exact own-`external_id` and the empty/hallucinated → own-channel fallback, while making it impossible to post to a channel the acting business does not own. It errors with `channel_not_found` only when the owned channel is itself unusable.

Telegram accepts either a numeric ID (private channels) or a public `@channelusername`; the connect flow stores `external_id` in the same form. `bot.go` picks `NewMessage`/`NewPhoto` for numeric IDs and `NewMessageToChannel`/`NewPhotoToChannel` for `@username`. Do **not** force `strconv.ParseInt` on the target — that rejects public-channel usernames.

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
