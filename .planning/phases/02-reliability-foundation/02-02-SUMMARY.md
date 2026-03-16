---
plan: "02-02"
title: "Error taxonomy across all agents"
status: complete
completed: "2026-03-16"
---

# PLAN-2.2 Summary: Error taxonomy across all agents

## What was done

Applied error classification to all three agent handlers (VK, Telegram, Yandex.Business) so that permanent platform errors are wrapped in `a2a.NonRetryableError` and skip retry logic, while transient errors remain retryable.

### Changes by agent

**VK Agent** (`services/agent-vk/internal/agent/handler.go`):
- Added `classifyVKError` function that checks VK SDK `*vkapi.Error` codes
- Permanent codes (5, 15, 100, 113) and rate-limit codes (6, 9) wrapped as NonRetryableError
- Token fetch errors wrapped as NonRetryableError
- 5 classification tests added

**Telegram Agent** (`services/agent-telegram/internal/agent/handler.go`):
- Added `classifyTelegramError` function using string matching on error messages
- Unauthorized, Forbidden, rate-limit, and chat-not-found errors wrapped as NonRetryableError
- Token fetch and sender factory errors wrapped as NonRetryableError
- 5 classification tests added

**Yandex.Business Agent** (`services/agent-yandex-business/internal/agent/handler.go`):
- Added `classifyYandexError` function using string matching on error messages
- Session expired, login redirect, and CAPTCHA errors wrapped as NonRetryableError
- Token fetch errors wrapped as NonRetryableError
- 5 classification tests added (already had `withRetry` integration with NonRetryableError in `browser.go`)

## Files modified

- `services/agent-vk/internal/agent/handler.go`
- `services/agent-vk/internal/agent/handler_test.go`
- `services/agent-telegram/internal/agent/handler.go`
- `services/agent-telegram/internal/agent/handler_test.go`
- `services/agent-yandex-business/internal/agent/handler.go`
- `services/agent-yandex-business/internal/agent/handler_test.go`

## Verification

- All 15 classification tests pass (5 per agent)
- All existing agent tests continue to pass
- All three agents build with `GOWORK=off go build ./...`
