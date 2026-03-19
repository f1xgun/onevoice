---
plan: "05-03"
title: "pkg/logger JSON output with service/env/version fields"
status: complete
completed: "2026-03-20"
---

# Summary: pkg/logger JSON output with service/env/version fields

## What was done

1. **correlation.go** — Added `WithCorrelationID` and `CorrelationIDFromContext` context helpers for attaching/extracting correlation IDs.

2. **context_handler.go** — Created `ContextHandler` wrapping `slog.Handler` that automatically injects `correlation_id` from context into every log record.

3. **logger.go** — Updated with:
   - `Config` struct (Service, Env, Version, Level)
   - `NewFromConfig(cfg Config)` for explicit configuration
   - `New(service)` reads `ENV`, `VERSION`, `LOG_LEVEL` env vars with sensible defaults
   - `NewWithLevel(service, level)` updated to use ContextHandler and env vars
   - `parseLogLevel` helper supporting DEBUG/INFO/WARN/ERROR (case-insensitive)

4. **logger_test.go** — 6 tests covering JSON output fields, correlation ID injection/absence, custom level filtering, and round-trip context helpers.

## Verification

- All 5 services compile without changes to their `logger.New(...)` call sites
- `go test -race ./logger/...` passes (6/6 tests)
- `go build ./...` succeeds for pkg

## Files modified

- `pkg/logger/correlation.go` (new)
- `pkg/logger/context_handler.go` (new)
- `pkg/logger/logger.go` (updated)
- `pkg/logger/logger_test.go` (new)
