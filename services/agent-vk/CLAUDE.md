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

## Tool Names

All tools prefixed with `vk__`:
- `vk__create_wall_post`
- `vk__get_wall_posts`
- `vk__delete_wall_post`
- `vk__get_community_info`

## A2A Pattern

Same as Telegram agent — NATS subscription, A2A protocol, tool dispatch.

## Build & Test

```bash
cd services/agent-vk && GOWORK=off go test -race ./...
cd services/agent-vk && golangci-lint run --config ../../.golangci.yml ./...
```
