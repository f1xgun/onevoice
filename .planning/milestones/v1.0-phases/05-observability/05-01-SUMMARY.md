---
plan: "05-01"
title: "Health check endpoints /health/live and /health/ready"
status: complete
completed: "2026-03-20"
commits:
  - "feat(05-01): create pkg/health shared health check package"
  - "feat(05-01): add unit tests for pkg/health"
  - "feat(05-01): wire health checks into API service"
  - "feat(05-01): wire health checks into orchestrator service"
  - "feat(05-01): add minimal HTTP health server to agent services"
---

# Plan 05-01 Summary: Health Check Endpoints

## What was done

1. **pkg/health/health.go** — Shared health check package with `Checker` type, `LiveHandler` (always 200 `{"status":"alive"}`), and `ReadyHandler` (runs registered checks, returns 200 or 503).

2. **pkg/health/health_test.go** — Four unit tests covering live handler, ready with all healthy, one failing, and no checks.

3. **API service** — Wired postgres, mongodb, redis checks into `/health/live` and `/health/ready` on both public and internal routers. Replaced old hardcoded "OK" response.

4. **Orchestrator service** — Added NATS health check to `/health/live` and `/health/ready`. Replaced old `{"status":"ok"}` response.

5. **Agent services** (telegram, vk, yandex-business) — Added minimal HTTP health servers on configurable `HEALTH_PORT` (defaults: 8081, 8082, 8083) with NATS check. Health server shuts down gracefully before NATS drain.

## Verification

- `cd pkg && go test -race ./health/...` passes
- All 5 services compile with `GOWORK=off go build ./...`
- `/health/live` returns 200 always
- `/health/ready` returns 200 when dependencies healthy, 503 otherwise
- `/health` kept as backward-compatible alias for liveness
