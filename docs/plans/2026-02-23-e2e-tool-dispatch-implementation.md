# E2E Tool Dispatch, Photo Support, and Tool Call Persistence — Implementation Notes

**Date:** 2026-02-23
**PR:** #15 — `feat: e2e tool dispatch, photo support, and tool call persistence`

## What Was Built

### pkg/llm — OpenRouter Tool Call Support
- `providers/openrouter.go`: map `ToolCalls` and `ToolCallID` fields when building chat completion requests and parsing responses
- Enables multi-turn tool call history to be correctly relayed to the LLM on subsequent iterations

### services/orchestrator — Tool Result Events + Photo Tool + Prompt
- `orchestrator.go`: added `EventToolResult` event type emitted after each tool execution with `ToolName`, `ToolResult`, `ToolError`
- `handler/chat.go`: SSE handler forwards `tool_result` events to frontend
- `cmd/main.go`: registered `telegram__send_channel_photo` tool; updated `send_channel_post` and `send_channel_photo` descriptions to be mutually exclusive (prevents LLM double-posting)
- `natsexec/executor.go`: fixed tool name passed in `ToolRequest.Tool` (was sending full NATS subject, now sends plain tool name)
- `prompt/builder.go`: tightened system prompt — act immediately, don't narrate plans

### services/agent-telegram — Photo Support + Robust Channel ID Resolution
- `agent/handler.go`:
  - `TokenInfo` struct with `AccessToken` + `ExternalID`
  - `getSender` returns resolved `ExternalID` from integration as third return value
  - All handlers fall back to resolved numeric ID when LLM passes non-numeric `channel_id`
  - Added `sendChannelPhoto` handler for `telegram__send_channel_photo`
- `telegram/bot.go`: added `SendPhoto(chatID, photoURL, caption)` using `tgbotapi.FileURL`
- `cmd/main.go`: `tokenAdapter.GetToken` returns `agentpkg.TokenInfo`

### services/api — Integration Fallback + Tool Call Persistence + Messages Endpoint
- `service/integration.go` — `GetDecryptedToken` falls back to first active integration when `externalID` not found
- `handler/chat_proxy.go` — accumulates `tool_call`/`tool_result` SSE events, saves as `ToolCalls`/`ToolResults` on the persisted Message
- `handler/conversation.go` — new `GET /conversations/:id/messages` endpoint returns full message history with tool calls
- `router/router.go` — wired new endpoint

### services/frontend — Tool Call Panel + Re-Entry Fix
- `app/(app)/chat/page.tsx` — per-message tool call panel with expandable details, TG/VK/YB badges, ✓/✗ counts
- `hooks/useChat.ts` — loads `toolCalls`/`toolResults` from messages API on mount; maps to `Message.toolCalls`; handles `tool_result` SSE events
- `components/chat/ChatWindow.tsx` — passes tool call data through

## Bugs Fixed
1. **NATS executor sent wrong tool name** — was sending full subject `tasks.telegram` instead of `telegram__send_channel_post`
2. **Empty `channel_id` → integration not found** — `GetDecryptedToken` now falls back to first active integration
3. **Non-numeric `channel_id`** — agent handlers now use resolved numeric ID from integration as fallback
4. **`tool_result` events not emitted** — orchestrator now always emits `EventToolResult` after each tool call
5. **Tool calls disappear on conversation re-entry** — chat proxy now persists them; frontend loads from API
6. **LLM sends two posts when photo requested** — fixed by making tool descriptions mutually exclusive

## CI Fixes Applied
- `unconvert`: removed `a2a.AgentID()` cast around `string` in `natsexec/executor.go`
- `exhaustive`: added `EventToolResult` case to switch in `orchestrator_test.go`
- Prettier: auto-formatted `chat/page.tsx` and `hooks/useChat.ts`
