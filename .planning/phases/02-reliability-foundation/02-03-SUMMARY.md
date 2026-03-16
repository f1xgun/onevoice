---
plan: "02-03"
title: "Graceful shutdown for all services"
phase: 2
wave: 1
status: complete
completed: "2026-03-16"
---

# SUMMARY: PLAN-2.3 — Graceful shutdown for all services

## What was done

### Task 1: Added WaitGroup and Stop() to pkg/a2a Agent
- Added `wg sync.WaitGroup` field to `Agent` struct
- Wrapped goroutines in `Start()` with `wg.Add(1)` / `defer wg.Done()`
- Added `Stop()` method that calls `wg.Wait()` for in-flight handler drain

### Task 2: Added Agent Stop() unit tests
- `TestAgent_Stop_WaitsForInflight` — handler sleeps 200ms, Stop() blocks >= 200ms
- `TestAgent_Stop_NoInflight` — Stop() returns immediately (< 50ms)
- Both tests use `fakeTransport` mock for direct subscription callback invocation

### Task 3: Added graceful shutdown to orchestrator
- Added `SHUTDOWN_TIMEOUT` to orchestrator config (default 30s, parsed via `time.ParseDuration`)
- Replaced blocking `ListenAndServe()` with signal-based shutdown pattern
- Shutdown order: HTTP `srv.Shutdown()` -> NATS `nc.Drain()` -> log stop

### Task 4: Updated agent services to use transport.Close() and agent.Stop()
- All three agents (telegram, vk, yandex-business) now call `transport.Close()` + `ag.Stop()` after `<-ctx.Done()`
- Removed `defer nc.Close()` in favor of explicit `transport.Close()` (which calls `nc.Drain()`)
- Ensures in-flight NATS handlers complete before process exit

### Task 5: Improved API service shutdown ordering
- Replaced hardcoded `30*time.Second` with configurable `cfg.ShutdownTimeout`
- Added `SHUTDOWN_TIMEOUT` to API config (default 30s)
- Moved `nc` variable to outer scope so it's accessible at shutdown time
- Removed `defer nc.Close()`, `defer pgPool.Close()`, `defer mongoClient.Disconnect()`
- Added ordered shutdown: HTTP stop -> NATS drain -> pgPool close -> mongoClient disconnect
- Changed srv.Shutdown error from fatal return to logged warning (allow cleanup to continue)

## Verification

- `cd pkg && go test -race ./a2a/` — PASS (4 tests including 2 Stop tests)
- `cd services/orchestrator && GOWORK=off go build ./...` — PASS
- `cd services/api && GOWORK=off go build ./...` — PASS
- `cd services/agent-telegram && GOWORK=off go build ./...` — PASS
- `cd services/agent-vk && GOWORK=off go build ./...` — PASS
- `cd services/agent-yandex-business && GOWORK=off go build ./...` — PASS
- `make lint-all` — golangci-lint not installed locally (CI-only tool)

## Files modified
- `pkg/a2a/agent.go` (modified — WaitGroup, Stop())
- `pkg/a2a/agent_test.go` (new — Stop tests)
- `services/orchestrator/cmd/main.go` (modified — signal shutdown)
- `services/orchestrator/internal/config/config.go` (modified — ShutdownTimeout)
- `services/agent-telegram/cmd/main.go` (modified — transport.Close + ag.Stop)
- `services/agent-vk/cmd/main.go` (modified — transport.Close + ag.Stop)
- `services/agent-yandex-business/cmd/main.go` (modified — transport.Close + ag.Stop)
- `services/api/cmd/main.go` (modified — ordered shutdown, nc.Drain, DB close)
- `services/api/internal/config/config.go` (modified — ShutdownTimeout)
