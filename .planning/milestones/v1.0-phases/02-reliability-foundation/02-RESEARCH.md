# Phase 2: Reliability Foundation — Research

**Researcher:** Claude
**Date:** 2026-03-15
**Phase requirements:** REL-01, REL-02, REL-03, REL-04

---

## 1. Current State Analysis

### 1.1 Graceful Shutdown (REL-01)

**API service** (`services/api/cmd/main.go`): Already has partial shutdown — listens for SIGINT/SIGTERM, shuts down both HTTP servers with a 30s timeout. However:
- NATS connection (optional, for review syncer) closes via `defer nc.Close()` — no drain.
- Redis, PostgreSQL, MongoDB close via `defer` — happens after `run()` returns, no ordered drain.
- Review syncer goroutine cancelled via `defer syncCancel()` but no wait for completion.
- The `go h.syncer.SyncBusiness(...)` calls in business handler are fire-and-forget with no WaitGroup — could be mid-flight at shutdown.

**Orchestrator** (`services/orchestrator/cmd/main.go`): No shutdown handling at all. `srv.ListenAndServe()` blocks forever; SIGTERM kills the process immediately. NATS connection closed only via `defer nc.Close()` (no drain). SSE connections in-flight will be severed.

**Telegram agent** (`services/agent-telegram/cmd/main.go`): Uses `signal.NotifyContext` correctly — context cancels on SIGTERM, `<-ctx.Done()` blocks. But: NATS `defer nc.Close()` does not drain; in-flight message handlers (`go a.handle(...)` spawned per message in `Agent.Start`) have no WaitGroup and may be interrupted.

**VK agent** (`services/agent-vk/cmd/main.go`): Same pattern as Telegram — signal.NotifyContext + defer nc.Close(). Same gaps.

**Yandex.Business agent** (`services/agent-yandex-business/cmd/main.go`): Same pattern. Additional concern: Playwright browser sessions may be left open mid-operation if shutdown happens during RPA.

**NATSTransport** (`pkg/a2a/nats_transport.go`): `Close()` already calls `nc.Drain()` which is the correct graceful approach. But no service currently calls `transport.Close()` — they all call `nc.Close()` directly.

### 1.2 Error Taxonomy (REL-02, REL-03)

**No NonRetryableError type exists.** The `pkg/a2a/protocol.go` defines `ToolRequest`/`ToolResponse` and agent IDs. No error types.

**Agent error handling — current state:**

- **Telegram handler** (`services/agent-telegram/internal/agent/handler.go`): All errors wrapped with `fmt.Errorf("telegram: ...: %w", err)`. No classification — token fetch failure, send failure, and parse errors all returned the same way. Caller (`a2a.Agent.handle`) converts any non-nil error to `ToolResponse{Success: false, Error: err.Error()}`.

- **VK handler** (`services/agent-vk/internal/agent/handler.go`): Same pattern. VK SDK errors (e.g., error code 5 = invalid token, error code 29 = rate limit) are wrapped generically with `fmt.Errorf("vk: ...: %w", err)`. No distinction between permanent and transient.

- **VK client** (`services/agent-vk/internal/vk/client.go`): Uses `vksdk/v3` which returns `vkapi.Error` type with `Code` field. Errors propagated as-is through wrapping.

- **Yandex.Business handler** (`services/agent-yandex-business/internal/agent/handler.go`): Errors from browser operations wrapped with `fmt.Errorf("yandex: ...: %w", err)`.

- **Yandex browser** (`services/agent-yandex-business/internal/yandex/browser.go`): `withRetry` retries ALL errors unconditionally — no short-circuit for permanent failures (e.g., session expired, element not found permanently). This is the primary integration point for NonRetryableError.

**Orchestrator error flow:** `natsexec/executor.go` line 59 wraps agent errors as `fmt.Errorf("natsexec: agent %s error: %s", ...)` — note this uses `%s` not `%w`, so error type information is lost at NATS boundary. This is expected since errors cross JSON serialization (ToolResponse.Error is a string). Error classification must happen at the agent level, before the NATS response.

### 1.3 Panic Calls (REL-04)

**Complete inventory of production panic() calls:**

| File | Function | Panic reason |
|------|----------|-------------|
| `services/api/internal/handler/auth.go:41` | `NewAuthHandler` | `userService cannot be nil` |
| `services/api/internal/handler/business.go:66` | `NewBusinessHandler` | `businessService cannot be nil` |
| `services/api/internal/handler/integration.go:32,35` | `NewIntegrationHandler` | `integrationService cannot be nil`, `businessService cannot be nil` |
| `services/api/internal/handler/conversation.go:33,36` | `NewConversationHandler` | `conversationRepo cannot be nil`, `messageRepo cannot be nil` |
| `services/api/internal/handler/review.go:39` | `NewReviewHandler` | `reviewService cannot be nil` |
| `services/api/internal/handler/post.go:37` | `NewPostHandler` | `postService cannot be nil` |
| `services/api/internal/handler/agent_task.go:35` | `NewAgentTaskHandler` | `agentTaskService cannot be nil` |
| `services/api/internal/service/user.go:48` | `NewUserService` | `jwt secret must be at least 32 bytes` |

**Total: 8 constructors, 10 panic() calls** (integration handler has 2, conversation handler has 2).

**NOT panicking** (no changes needed): `NewOAuthHandler`, `NewChatProxyHandler`, `NewInternalTokenHandler` — these do not have panic guards.

All panics are in constructors called during startup in `services/api/cmd/main.go`. The chi `Recoverer` middleware would NOT catch these since they happen before the HTTP server starts. A nil service passed to a constructor would crash the process.

---

## 2. Implementation Approach per Requirement

### REL-03: NonRetryableError type in pkg/a2a

**What to build:**
- Add `NonRetryableError` struct to `pkg/a2a/protocol.go` (where types live)
- Must implement `error` interface and `Is()` method for `errors.Is()` compatibility
- Must support wrapping (embed original error)

**Design:**
```go
// NonRetryableError marks an error as permanent — withRetry must not retry it.
type NonRetryableError struct {
    Err error
}

func (e *NonRetryableError) Error() string { return e.Err.Error() }
func (e *NonRetryableError) Unwrap() error { return e.Err }
func (e *NonRetryableError) Is(target error) bool {
    _, ok := target.(*NonRetryableError)
    return ok
}

// NewNonRetryableError wraps err as a permanent failure.
func NewNonRetryableError(err error) *NonRetryableError {
    return &NonRetryableError{Err: err}
}
```

**Integration with withRetry** (`services/agent-yandex-business/internal/yandex/browser.go`):
Add `errors.Is(lastErr, &NonRetryableError{})` check inside the retry loop to break immediately.

### REL-02: Error taxonomy across agents

**Classification scheme (from CONTEXT.md decisions):**

| Category | Behavior | Examples |
|----------|----------|---------|
| Transient | Retry with exponential backoff | Network timeout, NATS timeout, HTTP 5xx, Playwright navigation timeout |
| Permanent | Fail immediately (NonRetryableError) | VK error 5 (invalid token), 15 (access denied), 100 (invalid parameter); Telegram 401 (unauthorized), 403 (forbidden); Yandex session expired / login redirect |
| Rate-limited | Backoff with longer delay, surface on all fail | VK error 6 (too many requests), Telegram 429, Yandex CAPTCHA |

**Per-agent implementation:**

1. **VK agent**: VK SDK returns `*vkapi.Error` with `.Code`. Add error classification in `handler.go` after each client call. Map VK error codes: 5, 15, 100, 113 (invalid user id) = permanent; 6, 9 (flood control) = rate-limited; everything else = transient.

2. **Telegram agent**: Telegram bot API returns errors with descriptions. Check for HTTP status codes: 401/403 = permanent (wrap in NonRetryableError); 429 = rate-limited; network errors = transient.

3. **Yandex.Business agent**: Check page URL after navigation for login redirects (session expired = permanent). Playwright timeout errors = transient. Element-not-found after canary check = permanent.

**Where errors surface:** Agent returns error via `a2a.ToolResponse.Error` string. Orchestrator relays this via SSE `tool_result` event with `tool_error` field. No new event types needed (confirmed in CONTEXT.md).

### REL-01: Graceful shutdown for all services

**Shutdown drain order** (from CONTEXT.md): HTTP server stop -> NATS drain -> flush writes -> close DB pools.

**Per-service changes:**

1. **Orchestrator** (`services/orchestrator/cmd/main.go`) — needs the most work:
   - Add `signal.NotifyContext` or signal channel
   - Run HTTP server in goroutine, wait for signal
   - Call `srv.Shutdown(ctx)` with 30s timeout
   - Call `nc.Drain()` (not `nc.Close()`)
   - SSE connections: `srv.Shutdown` will wait for active requests to complete; SSE handlers must respect context cancellation

2. **API service** (`services/api/cmd/main.go`) — partially done:
   - Already has signal handling and HTTP shutdown
   - Add: NATS drain before close (currently just `nc.Close()`)
   - Add: Wait for fire-and-forget goroutines (`go syncer.SyncBusiness`) — add a `sync.WaitGroup` or accept data loss for these async syncs
   - Ordering: already correct (HTTP shutdown first, then deferred closes)

3. **Agent services** (telegram, vk, yandex-business) — all same pattern:
   - Already use `signal.NotifyContext` — good
   - Change `defer nc.Close()` to use `transport.Close()` (which calls `nc.Drain()`) instead
   - For Yandex agent: ensure Playwright browser cleanup on shutdown (browser.Close in withPage already handles individual requests, but a running withPage call at shutdown time needs context cancellation to propagate)
   - Add WaitGroup to `a2a.Agent` to track in-flight `go a.handle(...)` goroutines, drain on `Stop()`

**Agent base class change** (`pkg/a2a/agent.go`):
- Add `sync.WaitGroup` field
- Wrap `go a.handle(...)` with `wg.Add(1)` / `defer wg.Done()`
- Add `Stop()` method that calls `wg.Wait()` with timeout
- All agent cmd/main.go files call `ag.Stop()` after `<-ctx.Done()`

**Configurable timeout:** `SHUTDOWN_TIMEOUT` env var, default 30s, parsed in each service's config.

### REL-04: Replace panic() with error returns

**Change pattern for each constructor:**

Before:
```go
func NewAuthHandler(userService UserService, secureCookies bool) *AuthHandler {
    if userService == nil {
        panic("userService cannot be nil")
    }
    return &AuthHandler{...}
}
```

After:
```go
func NewAuthHandler(userService UserService, secureCookies bool) (*AuthHandler, error) {
    if userService == nil {
        return nil, fmt.Errorf("NewAuthHandler: userService cannot be nil")
    }
    return &AuthHandler{...}, nil
}
```

**Cascade to main.go:** `services/api/cmd/main.go` must handle errors from all 7 handler constructors + 1 service constructor. Pattern:
```go
authHandler, err := handler.NewAuthHandler(userService, cfg.SecureCookies)
if err != nil {
    return fmt.Errorf("init auth handler: %w", err)
}
```

This propagates up to `run() error` which already logs and exits with code 1 — same behavior as panic but cleaner.

**Files requiring signature changes:**
- `services/api/internal/handler/auth.go` — NewAuthHandler
- `services/api/internal/handler/business.go` — NewBusinessHandler
- `services/api/internal/handler/integration.go` — NewIntegrationHandler
- `services/api/internal/handler/conversation.go` — NewConversationHandler
- `services/api/internal/handler/review.go` — NewReviewHandler
- `services/api/internal/handler/post.go` — NewPostHandler
- `services/api/internal/handler/agent_task.go` — NewAgentTaskHandler
- `services/api/internal/service/user.go` — NewUserService
- `services/api/cmd/main.go` — caller side, handle all returned errors

---

## 3. Codebase Integration Points

### pkg/a2a/protocol.go
- Add `NonRetryableError` struct and `NewNonRetryableError` constructor
- Add tests in `pkg/a2a/protocol_test.go`

### pkg/a2a/agent.go
- Add `sync.WaitGroup` to `Agent` struct
- Track goroutines in `handle()` calls
- Add `Stop()` method for graceful drain
- Update tests in `pkg/a2a/agent_test.go`

### services/agent-telegram/internal/agent/handler.go
- Classify errors from `sender.SendMessage`/`sender.SendPhoto` (Telegram API errors)
- Classify token fetch errors (token not found = permanent)

### services/agent-vk/internal/agent/handler.go
- Import VK SDK error types
- Classify VK API errors by code in each handler method
- Wrap permanent errors with `a2a.NewNonRetryableError()`

### services/agent-vk/internal/vk/client.go
- Preserve VK error type information (currently wraps with `fmt.Errorf` using `%w` — good)

### services/agent-yandex-business/internal/yandex/browser.go
- Modify `withRetry` to check `errors.Is(lastErr, &a2a.NonRetryableError{})` — break loop
- Import `pkg/a2a`

### services/agent-yandex-business/internal/agent/handler.go
- Classify Playwright/browser errors (session expired = permanent)

### services/api/internal/handler/ (7 files)
- Change constructor signatures from `*XxxHandler` to `(*XxxHandler, error)`
- Replace `panic()` with `return nil, fmt.Errorf(...)`

### services/api/internal/service/user.go
- Change `NewUserService` signature to return `(UserService, error)`
- Update `UserService` interface if it has a constructor contract (it doesn't — just the concrete function)

### services/api/cmd/main.go
- Handle errors from all changed constructors
- No structural changes to shutdown (already correct)
- Consider adding NATS drain if present

### services/orchestrator/cmd/main.go
- Add signal handling and graceful shutdown (major change)
- Add NATS drain

### services/agent-*/cmd/main.go (3 files)
- Use `transport.Close()` instead of `nc.Close()`
- Call `ag.Stop()` after context cancellation
- Add shutdown timeout logging

---

## 4. Dependencies and Ordering

**Recommended implementation order:**

1. **PLAN-2.1: NonRetryableError** (REL-03) — zero dependencies, other plans depend on it
   - Add type to `pkg/a2a/protocol.go`
   - Integrate with `withRetry` in yandex browser.go
   - Tests

2. **PLAN-2.2: Error taxonomy** (REL-02) — depends on PLAN-2.1
   - Apply classification to all 3 agent handlers
   - VK error code mapping
   - Telegram error classification
   - Yandex session-expired detection
   - Tests per agent

3. **PLAN-2.3: Graceful shutdown** (REL-01) — independent of 2.1/2.2 but best done after
   - Agent WaitGroup in `pkg/a2a/agent.go`
   - Orchestrator shutdown
   - Agent cmd/main.go updates (3 files)
   - API service NATS drain improvement
   - Tests

4. **PLAN-2.4: Panic removal** (REL-04) — fully independent, can run in parallel with anything
   - Constructor signature changes (8 constructors)
   - main.go caller updates
   - Tests

**Critical path:** PLAN-2.1 -> PLAN-2.2 (serial). PLAN-2.3 and PLAN-2.4 can run in parallel with each other and with 2.2.

---

## 5. Risks and Gotchas

### 5.1 withRetry import cycle
`services/agent-yandex-business/internal/yandex/browser.go` importing `pkg/a2a` for `NonRetryableError`. Currently `browser.go` only imports `playwright-go` and stdlib. Adding `pkg/a2a` import is fine since all service modules already depend on `pkg/` via `go.mod replace`.

### 5.2 SSE connections during shutdown
The orchestrator's `handler/chat.go` streams SSE responses. `http.Server.Shutdown` waits for active connections. SSE connections are long-lived — the shutdown timeout (30s) must be sufficient for in-flight LLM calls to complete. The orchestrator's agent loop has `MAX_ITERATIONS=10` guard. If an LLM call is mid-flight at shutdown, the context cancellation should propagate through the HTTP request context to the LLM provider call.

Mitigation: Ensure `chatHandler.Chat` respects `r.Context()` cancellation and breaks the agent loop.

### 5.3 NATS Drain semantics
`nc.Drain()` is asynchronous — it signals the connection to stop accepting new messages and waits for in-flight to complete. But the function returns immediately. Need to pair with `nc.FlushTimeout()` or a sleep/wait, or rely on the `Agent.Stop()` WaitGroup to confirm all handlers completed.

### 5.4 VK SDK error type assertions
The VK SDK `vksdk/v3` wraps errors as `*vkapi.Error`. Need to verify that `errors.As(err, &vkErr)` works through the `fmt.Errorf("vk wall.post: %w", err)` wrapping in `client.go`. It should — `%w` preserves the error chain.

### 5.5 Constructor signature changes are breaking
Changing `New*Handler() *Handler` to `New*Handler() (*Handler, error)` is a breaking API change. All callers must be updated simultaneously. In this codebase there is only one caller (`services/api/cmd/main.go`) so the blast radius is contained. But tests that call these constructors directly must also be updated.

### 5.6 Telegram bot-api error types
The `go-telegram-bot-api/v5` library returns `tgbotapi.Error` with `Code` and `Message` fields. Need to check: does the current `telegram.New(botToken)` wrapper preserve these error types? If it wraps them, `errors.As` must still work.

### 5.7 Agent goroutines and context cancellation
In `pkg/a2a/agent.go`, the `go a.handle(ctx, reply, data)` goroutines receive the parent context. When the agent's context is cancelled (shutdown), these goroutines should check `ctx.Err()` and bail. Currently `handler.Handle(ctx, req)` passes context through — the actual platform API calls may or may not respect cancellation. For Telegram/VK (HTTP calls), stdlib net/http respects context. For Yandex (Playwright), context cancellation is noted in the code comment as unsupported by Playwright.

---

## 6. Validation Architecture

### Unit Tests

**NonRetryableError:**
- `errors.Is(NewNonRetryableError(someErr), &NonRetryableError{})` returns true
- `errors.Is(someErr, &NonRetryableError{})` returns false
- `errors.Unwrap()` returns original error
- `Error()` returns original message

**withRetry + NonRetryableError:**
- withRetry stops immediately when fn returns NonRetryableError
- withRetry continues retrying for non-NonRetryableError
- withRetry still respects context cancellation

**Error classification (per agent):**
- VK: mock VKClient returning VK error code 5 -> handler returns NonRetryableError
- VK: mock VKClient returning VK error code 6 -> handler returns normal error (retryable)
- Telegram: mock Sender returning 401 -> NonRetryableError
- Telegram: mock Sender returning network error -> normal error

**Constructor error returns:**
- `NewAuthHandler(nil, false)` returns error (not panic)
- `NewAuthHandler(validService, false)` returns handler + nil
- All 8 constructors tested for nil-dependency error path

### Integration-Level Tests

**Graceful shutdown:**
- Start service, send SIGTERM, verify clean exit (exit code 0)
- Start service with in-flight request, send SIGTERM, verify request completes before exit
- Agent WaitGroup: start agent, send message, immediately cancel context, verify handler completes

**Shutdown timeout:**
- Set SHUTDOWN_TIMEOUT=1s, start service with slow handler, send SIGTERM, verify forced exit after 1s

### Verification Commands
```bash
make lint-all        # Verify no new lint issues
make test-all        # All tests pass
# Manual: check no panic() in handler/service constructors
grep -r 'panic(' services/api/internal/handler/ services/api/internal/service/user.go
```

---

## RESEARCH COMPLETE
