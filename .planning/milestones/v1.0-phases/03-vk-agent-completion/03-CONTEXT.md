# Phase 3: VK Agent Completion - Context

**Gathered:** 2026-03-16
**Status:** Ready for planning

<domain>
## Phase Boundary

Bring VK agent to full feature parity with Telegram: photo posts (two-step upload), scheduled posts (VK native), comment reply/delete, community info reads, and wall post reads. Includes integration tests with mock VK API server. All new tools registered in orchestrator.

</domain>

<decisions>
## Implementation Decisions

### VK API Tool Design
- Two-step photo upload: `photos.getWallUploadServer` → upload to URL → `photos.saveWallPhoto` → `wall.post` with attachment
- Scheduling via VK's native `publish_date` parameter in `wall.post` — no local cron/scheduler needed
- Tool naming follows existing convention: `vk__post_photo`, `vk__schedule_post`, `vk__reply_comment`, `vk__delete_comment`, `vk__get_community_info`, `vk__get_wall_posts`
- Client-side rate limiter respecting VK's 3 req/sec limit — surface 429 via NonRetryableError with "rate_limited" category

### Mock Server & Test Strategy
- `httptest.Server` with VK-compatible JSON responses — no external dependencies, fast, deterministic
- Error paths tested: permanent (Error 5 invalid token), transient (network timeout), rate-limited (Error 6)
- Photo upload endpoint mocked too — returns fake photo ID, verifies `wall.post` receives correct attachment
- All 6 VK tools covered by integration tests

### Claude's Discretion
- Exact VK API response format parsing (nested `response` object structure)
- How to pass `owner_id` (negative for communities) — standard VK convention
- Whether to use VK SDK or raw HTTP calls (SDK preferred if available in go.mod)
- Pagination strategy for wall reads (offset-based vs cursor)
- Comment thread depth handling

</decisions>

<code_context>
## Existing Code Insights

### Reusable Assets
- `services/agent-vk/internal/agent/handler.go` — existing VK agent with NATS handler, `classifyVKError` (from Phase 2)
- `pkg/a2a/types.go` — NonRetryableError for error classification
- `services/agent-telegram/internal/agent/handler.go` — reference for tool implementation patterns
- `services/orchestrator/internal/tools/registry.go` — tool registration pattern

### Established Patterns
- Tool naming: `{platform}__{action}` (e.g., `telegram__send_channel_post`)
- NATS subjects: `tasks.{agentID}` (VK agent = `tasks.vk`)
- Token lookup: `GetDecryptedToken(ctx, businessID, platform, externalID)`
- Tool dispatch: orchestrator → NATS → agent handler → switch on action

### Integration Points
- `services/agent-vk/internal/agent/handler.go` — add new tool handlers
- `services/orchestrator/internal/tools/registry.go` — register 6 new VK tools
- `services/orchestrator/cmd/main.go` — wire VK tools into registry
- VK API base URL: `https://api.vk.com/method/`

</code_context>

<specifics>
## Specific Ideas

No specific requirements — standard VK API integration patterns apply.

</specifics>

<deferred>
## Deferred Ideas

- VK Stories publishing (VK-07) — deferred to v2
- VK community chat messages (VK-08) — deferred to v2
- VK analytics/stats (VK-09) — deferred to v2

</deferred>
