---
phase: "05"
title: "Observability"
goal: "Make system health and performance visible — health checks, Prometheus metrics, structured JSON logging, and correlation IDs across services."
verified: "2026-03-20"
result: PASS
---

# Phase 05 Observability — Verification Report

## Summary

All 4 requirements (OBS-01 through OBS-04) are fully implemented. Every file claimed in the
SUMMARY documents exists and contains the code described. All 4 plans are marked complete in
ROADMAP.md.

---

## OBS-01: Health Check Endpoints

**Result: PASS**

### Files confirmed

- `pkg/health/health.go` — `Checker` type, `LiveHandler()` (always 200 `{"status":"alive"}`),
  `ReadyHandler()` (runs registered checks, returns 200 `{"status":"ready","checks":{...}}` or
  503 `{"status":"not_ready","checks":{...}}`).
- `pkg/health/health_test.go` — 4 unit tests: always-200 live, all-healthy ready, one-failing
  ready (503 + `not_ready`), no-checks ready (200).

### Routes confirmed

| Service | File | Routes |
|---------|------|--------|
| API (public) | `services/api/internal/router/router.go:127-129` | `/health/live`, `/health/ready`, `/health` |
| API (internal) | `services/api/internal/router/router.go:143-145` | `/health/live`, `/health/ready`, `/health` |
| Orchestrator | `services/orchestrator/cmd/main.go:103-105` | `/health/live`, `/health/ready`, `/health` |
| agent-telegram | `services/agent-telegram/cmd/main.go:55-57` | `/health/live`, `/health/ready`, `/health` — port 8081 |
| agent-vk | `services/agent-vk/cmd/main.go:55-57` | `/health/live`, `/health/ready`, `/health` — port 8082 |
| agent-yandex-business | `services/agent-yandex-business/cmd/main.go:55-57` | `/health/live`, `/health/ready`, `/health` — port 8083 |

### Dependency checks registered

- API: `postgres`, `mongodb`, `redis` (`services/api/cmd/main.go:85-91`)
- Orchestrator: `nats` (`services/orchestrator/cmd/main.go:70`)
- All 3 agent services: `nats`

### Success criterion note

ROADMAP success criterion says `"status": "degraded"` for a failing check; the actual
implementation uses `"status": "not_ready"`. The behavior is correct (503 + individual check
results identifying the failing dependency); only the string value differs from the criterion
wording. This is a cosmetic mismatch in the success criterion text, not a functional defect.

---

## OBS-02: Prometheus Metrics on /metrics

**Result: PASS**

### Files confirmed

- `pkg/metrics/middleware.go` — `HTTPMiddleware` chi middleware recording `http_requests_total`
  (CounterVec) and `http_request_duration_seconds` (HistogramVec) with labels `method/path/status`.
  Uses `chi.RouteContext().RoutePattern()` for path label to prevent cardinality explosion from
  path parameters (no UUIDs or user IDs in metric labels).
- `pkg/metrics/llm.go` — `RecordLLMRequest(model, provider, status, duration)` recording
  `llm_requests_total` and `llm_request_duration_seconds`.
- `pkg/metrics/tools.go` — `RecordToolDispatch(tool, agent, status, duration)` recording
  `tool_dispatch_total` and `tool_dispatch_duration_seconds`.
- `pkg/metrics/middleware_test.go` — unit tests for middleware.

### /metrics endpoint wiring

| Service | File | Middleware | Endpoint |
|---------|------|-----------|----------|
| API | `services/api/internal/router/router.go:52,124` | `metrics.HTTPMiddleware` global | `r.Handle("/metrics", promhttp.Handler())` |
| Orchestrator | `services/orchestrator/cmd/main.go:99,102` | `metrics.HTTPMiddleware` global | `r.Handle("/metrics", promhttp.Handler())` |

### Instrumentation call sites

- `pkg/llm/router.go:165,169,202,206` — `metrics.RecordLLMRequest()` called around all LLM
  provider calls in both `Chat()` and `ChatStream()`.
- `services/orchestrator/internal/orchestrator/orchestrator.go:163` — `metrics.RecordToolDispatch()`
  called in the tool dispatch loop.

---

## OBS-03: Structured JSON Logging with service/env/version Fields

**Result: PASS**

### Files confirmed

- `pkg/logger/logger.go` — `Config{Service, Env, Version, Level}`, `NewFromConfig(cfg)`,
  `New(service)` (reads `ENV`, `VERSION`, `LOG_LEVEL` env vars), `NewWithLevel(service, level)`.
  Uses `slog.NewJSONHandler` wrapped by `ContextHandler`.
- `pkg/logger/context_handler.go` — `ContextHandler` wrapping `slog.Handler`, injects
  `correlation_id` from context into every log record via `CorrelationIDFromContext`.
- `pkg/logger/correlation.go` — `WithCorrelationID(ctx, id)` and `CorrelationIDFromContext(ctx)`
  context helpers.
- `pkg/logger/logger_test.go` — 6 tests:
  - `TestNew_JSONOutput` — verifies `service`, `env`, `version`, `level` fields present in JSON output.
  - `TestContextHandler_CorrelationID` — verifies `correlation_id` injected when set in context.
  - `TestContextHandler_NoCorrelationID` — verifies `correlation_id` absent when not set.
  - `TestNewFromConfig_CustomLevel` — verifies debug suppression and warn appearance at WARN level.
  - `TestCorrelationID_RoundTrip` — context helper round-trip.
  - `TestCorrelationIDFromContext_Missing` — returns `""` when absent.

---

## OBS-04: Correlation ID Middleware and NATS Propagation

**Result: PASS**

### End-to-end flow confirmed

```
Client request
  → API CorrelationID middleware: reads X-Correlation-ID header or generates UUID
      stores in context via logger.WithCorrelationID, echoes in response header
      [services/api/internal/middleware/correlation.go]
  → API router: middleware.CorrelationID() applied to both Setup and SetupInternal chains
      [services/api/internal/router/router.go:39,138]
  → Chat proxy: reads correlation ID from context, sets X-Correlation-ID on proxy request to orchestrator
      [services/api/internal/handler/chat_proxy.go:165-166]
  → Orchestrator handler: reads X-Correlation-ID from incoming request, stores in context
      [services/orchestrator/internal/handler/chat.go:103-104]
  → NATSExecutor: reads correlation ID from context, sets ToolRequest.RequestID
      [services/orchestrator/internal/natsexec/executor.go:41]
  → NATS → Agent (pkg/a2a/agent.go:65-66): extracts req.RequestID, stores as correlation ID in handler context
  → All slog calls using that context automatically include correlation_id field (ContextHandler)
```

### Tests confirmed

- `services/api/internal/middleware/correlation_test.go` — 4 tests:
  - Generates UUID when header missing.
  - Preserves existing header value.
  - Injects into context.
  - Generated ID matches response header.
- `services/orchestrator/internal/natsexec/executor_test.go` — 2 tests:
  - `TestExecute_SetsRequestIDFromCorrelationID` — context correlation ID flows to `ToolRequest.RequestID`.
  - Absent correlation ID → empty `RequestID`.

---

## Phase Completion Assessment

| Requirement | Plan | Status | Evidence |
|-------------|------|--------|---------|
| OBS-01 | 05-01 | PASS | `pkg/health/health.go`, routes in all 5 services, 4 unit tests |
| OBS-02 | 05-02 | PASS | `pkg/metrics/middleware.go`, `/metrics` on API+orchestrator, LLM+tool instrumentation |
| OBS-03 | 05-03 | PASS | `pkg/logger/logger.go` JSON+context handler, 6 unit tests |
| OBS-04 | 05-04 | PASS | API middleware → proxy → orchestrator → NATS → agent chain, 6 unit tests |

**Phase 05 goal: ACHIEVED**
