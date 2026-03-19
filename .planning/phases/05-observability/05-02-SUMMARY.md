---
plan: "05-02"
title: "Prometheus metrics on /metrics"
status: complete
completed: "2026-03-20"
---

## Summary

Added Prometheus metrics instrumentation to the API and orchestrator services, exposing a `/metrics` endpoint on both services for scraping.

## What was done

### New files
- `pkg/metrics/middleware.go` — chi-compatible HTTP middleware recording `http_requests_total` (CounterVec) and `http_request_duration_seconds` (HistogramVec) with method/path/status labels. Uses `chi.RouteContext` for path labels to prevent cardinality explosion.
- `pkg/metrics/llm.go` — `RecordLLMRequest()` recording `llm_requests_total` (CounterVec) and `llm_request_duration_seconds` (HistogramVec) with model/provider/status labels.
- `pkg/metrics/tools.go` — `RecordToolDispatch()` recording `tool_dispatch_total` (CounterVec) and `tool_dispatch_duration_seconds` (HistogramVec) with tool/agent/status labels.
- `pkg/metrics/middleware_test.go` — Unit tests verifying metric recording and chi route pattern usage.

### Modified files
- `pkg/go.mod` — Added `github.com/prometheus/client_golang` and `github.com/go-chi/chi/v5` dependencies.
- `pkg/llm/router.go` — Instrumented `Chat()` and `ChatStream()` with `metrics.RecordLLMRequest()` around provider calls.
- `services/orchestrator/internal/orchestrator/orchestrator.go` — Instrumented tool dispatch loop with `metrics.RecordToolDispatch()`, extracting agent name from tool name prefix.
- `services/api/internal/router/router.go` — Added `metrics.HTTPMiddleware` to global middleware chain and mounted `/metrics` with `promhttp.Handler()`.
- `services/orchestrator/cmd/main.go` — Added `metrics.HTTPMiddleware` and mounted `/metrics` with `promhttp.Handler()`.

## Metrics exposed

| Metric | Type | Labels | Service |
|--------|------|--------|---------|
| `http_requests_total` | Counter | method, path, status | API, Orchestrator |
| `http_request_duration_seconds` | Histogram | method, path, status | API, Orchestrator |
| `llm_requests_total` | Counter | model, provider, status | Orchestrator (via pkg) |
| `llm_request_duration_seconds` | Histogram | model, provider | Orchestrator (via pkg) |
| `tool_dispatch_total` | Counter | tool, agent, status | Orchestrator |
| `tool_dispatch_duration_seconds` | Histogram | tool, agent | Orchestrator |

## Verification

- `cd pkg && go build ./...` — passes
- `cd services/api && GOWORK=off go build ./...` — passes
- `cd services/orchestrator && GOWORK=off go build ./...` — passes
- `cd pkg && go test -race ./metrics/...` — passes (2 tests)
