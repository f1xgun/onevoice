---
plan: "02-01"
title: "NonRetryableError type + withRetry integration"
phase: 2
wave: 1
status: complete
completed: "2026-03-15"
---

# SUMMARY: PLAN-2.1 — NonRetryableError type + withRetry integration

## What was done

### Task 1: Added NonRetryableError type to pkg/a2a/types.go
- Created `pkg/a2a/types.go` with `NonRetryableError` struct implementing `Error()`, `Unwrap()`, `Is()` methods
- Added `NewNonRetryableError(err)` constructor function

### Task 2: Added unit tests for NonRetryableError
- Created `pkg/a2a/types_test.go` with 5 test cases:
  - `TestNonRetryableError_Is` — errors.Is returns true for NonRetryableError
  - `TestNonRetryableError_IsNegative` — errors.Is returns false for normal errors
  - `TestNonRetryableError_Unwrap` — unwraps to the original error
  - `TestNonRetryableError_ErrorMessage` — preserves wrapped error message
  - `TestNonRetryableError_IsWrapped` — works through fmt.Errorf wrapping chains

### Task 3: Integrated NonRetryableError into withRetry
- Modified `services/agent-yandex-business/internal/yandex/browser.go`
- Added `errors.Is(lastErr, &a2a.NonRetryableError{})` check after `fn()` call
- Permanent errors now short-circuit immediately without exhausting retries

### Task 4: Added withRetry integration tests
- Created `services/agent-yandex-business/internal/yandex/browser_test.go` with 4 test cases:
  - `TestWithRetry_NonRetryableError_StopsImmediately` — 1 call only
  - `TestWithRetry_TransientError_RetriesAll` — calls fn maxAttempts times
  - `TestWithRetry_SuccessOnSecondAttempt` — 2 calls, returns nil
  - `TestWithRetry_ContextCancelled` — returns context error, 0 fn calls

## Verification

- `cd pkg && GOWORK=off go build ./...` — PASS
- `cd pkg && GOWORK=off go test -race ./a2a/` — PASS (all tests)
- `cd services/agent-yandex-business && GOWORK=off go build ./...` — PASS
- `cd services/agent-yandex-business && GOWORK=off go test -race -run TestWithRetry ./internal/yandex/` — PASS (4/4)
- `make lint-all` — golangci-lint not installed locally (CI-only tool)

## Files modified
- `pkg/a2a/types.go` (new)
- `pkg/a2a/types_test.go` (new)
- `services/agent-yandex-business/internal/yandex/browser.go` (modified)
- `services/agent-yandex-business/internal/yandex/browser_test.go` (new)
