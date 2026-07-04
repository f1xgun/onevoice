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

## Group ID Resolution Pattern

Both `getClient` (write) and `getReadClient` (read) resolve the target community via `resolveGroupTarget(supplied, resolved)`, where `resolved` is the acting business's own connected community (`info.ExternalID` from the business-scoped token resolver).

**Ownership rule (cross-tenant guard):** the read tools take `group_id` as a free-string LLM arg, and the read client can fall back to the shared `VK_SERVICE_KEY`, which reads **any** public community. An unverified LLM-supplied `group_id` must therefore never be trusted as the target — a hallucinated or prompt-injected value (e.g. quoted back from untrusted VK comment text) naming a *different* community would let the agent read a foreign community's wall and third-party commenter PII into the acting tenant's chat and audit records. `resolveGroupTarget` honors `supplied` **only when it exactly equals `resolved`** (a legitimate own-community target); every other supplied value (empty, hallucinated, or a foreign group) falls back to the owned `resolved` community. Writes are scoped the same way for uniformity (VK also scopes the community `AccessToken` to one community, so a foreign write fails closed at VK). The service-key vs user-token vs community-token client priority is unchanged — only the `group_id` target selection is guarded.

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
