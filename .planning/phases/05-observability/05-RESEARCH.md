# Phase 5: Observability — Research

**Researched:** 2026-03-20
**Requirements:** OBS-01, OBS-02, OBS-03, OBS-04

---

## 1. Current State

### Logging
- `pkg/logger/logger.go` already outputs JSON via `slog.NewJSONHandler` with a `service` field.
- Missing: `env` and `version` fields required by OBS-03.
- Missing: correlation ID injection into log context.
- All services call `logger.New("serviceName")` and `slog.SetDefault(log)` in their `cmd/main.go`.
- Agent services (telegram, vk, yandex-business) use bare `slog.Info`/`slog.Error` without structured logger setup — they rely on the default global logger.

### Health Checks
- API service has `GET /health` returning 200 with body `"OK"` (text, not JSON). Also duplicated on internal router.
- Orchestrator has `GET /health` returning `{"status":"ok"}` (JSON).
- Agent services (telegram, vk, yandex-business) have **no health endpoints** — they are NATS-only, no HTTP server.
- None of the existing health endpoints check downstream dependencies (no readiness probes).

### Metrics
- No Prometheus metrics anywhere. No `/metrics` endpoint.
- `chimiddleware.Logger` is used for request logging on both API and orchestrator routers, but this is chi's built-in text logger — not metrics.

### Correlation IDs
- `chimiddleware.RequestID` is used on API, orchestrator, and internal routers — generates a chi-level request ID, but it is not propagated downstream.
- `a2a.ToolRequest` already has a `RequestID string` field (`request_id,omitempty`), but the orchestrator's `natsexec.Execute()` never populates it.
- No `X-Correlation-ID` header is generated or forwarded.
- The chat proxy (`chat_proxy.go`) forwards to orchestrator via HTTP but does not pass any correlation header.

---

## 2. Health Check Design (OBS-01)

### Endpoints

Two endpoints per service:

| Endpoint | Semantics | Response |
|----------|-----------|----------|
| `/health/live` | Process is running and not deadlocked | Always 200 `{"status":"alive"}` |
| `/health/ready` | Process can serve traffic (all deps reachable) | 200 `{"status":"ready"}` or 503 `{"status":"not_ready","checks":{...}}` |

### Dependency Checks per Service

| Service | Liveness | Readiness Checks |
|---------|----------|------------------|
| API | Always 200 | PostgreSQL (`pgPool.Ping`), MongoDB (`mongoClient.Ping`), Redis (`redisClient.Ping`) |
| Orchestrator | Always 200 | NATS (`nc.Status() == Connected`), LLM provider reachable (optional — could be heavy) |
| Telegram Agent | Always 200 | NATS (`nc.Status() == Connected`) |
| VK Agent | Always 200 | NATS (`nc.Status() == Connected`) |
| Yandex.Business Agent | Always 200 | NATS (`nc.Status() == Connected`) |

### Implementation Approach

Create `pkg/health/` with a shared `Checker` type:

```go
// pkg/health/health.go
type CheckFunc func(ctx context.Context) error

type Checker struct {
    checks map[string]CheckFunc
}

func (c *Checker) AddCheck(name string, fn CheckFunc)
func (c *Checker) LiveHandler() http.HandlerFunc    // always 200
func (c *Checker) ReadyHandler() http.HandlerFunc   // runs all checks, 200 or 503
```

Each service creates a `Checker`, registers dependency checks, and mounts the two handlers.

### Agent Services — HTTP for Health Only

Agent services currently have no HTTP server. Options:
1. **Add a minimal HTTP server** just for health endpoints (simple, standard for k8s probes).
2. Use NATS-based health (non-standard, harder for k8s).

Recommendation: **Option 1** — add a lightweight HTTP server on a configurable `HEALTH_PORT` (default: 8081 for telegram, 8082 for vk, 8083 for yandex-business). This is idiomatic for k8s health probes and costs almost nothing.

### Migration of Existing `/health`

Replace the existing `/health` endpoints on API and orchestrator with `/health/live` and `/health/ready`. Keep a redirect or alias from `/health` -> `/health/live` for backward compat during rollout.

---

## 3. Prometheus Metrics (OBS-02)

### Library Choice

**`github.com/prometheus/client_golang`** — the de facto standard. Alternatives like VictoriaMetrics client are lighter but less ecosystem support. Stick with the canonical library.

Add to `pkg/go.mod` (shared) or per-service go.mod. Since metrics middleware is shared code, add to `pkg/`.

### Metrics to Export

#### API Service

| Metric | Type | Labels | Purpose |
|--------|------|--------|---------|
| `http_requests_total` | Counter | `method`, `path`, `status` | Request volume |
| `http_request_duration_seconds` | Histogram | `method`, `path`, `status` | Latency distribution |
| `http_request_size_bytes` | Histogram | `method`, `path` | Request payload size |
| `http_response_size_bytes` | Histogram | `method`, `path` | Response payload size |

#### Orchestrator Service

| Metric | Type | Labels | Purpose |
|--------|------|--------|---------|
| `http_requests_total` | Counter | `method`, `path`, `status` | Request volume |
| `http_request_duration_seconds` | Histogram | `method`, `path`, `status` | Latency distribution |
| `llm_requests_total` | Counter | `model`, `provider`, `status` | LLM call volume |
| `llm_request_duration_seconds` | Histogram | `model`, `provider` | LLM latency |
| `tool_dispatch_total` | Counter | `tool`, `agent`, `status` | Tool execution volume |
| `tool_dispatch_duration_seconds` | Histogram | `tool`, `agent` | Tool execution latency |
| `agent_loop_iterations_total` | Counter | `model` | Iterations per run |

### Label Hygiene

- **Path normalization**: Use chi's route pattern (`/api/v1/conversations/{id}`) not the actual URL (`/api/v1/conversations/abc-123`), to avoid cardinality explosion.
- **Status bucketing**: Use HTTP status code as-is (200, 400, 500) — finite cardinality.
- **Tool name**: Already finite (< 20 tools registered), safe as label.
- **Model/provider**: Small set, safe.
- Never use user ID, business ID, or conversation ID as labels.

### Implementation Approach

1. **`pkg/metrics/` package** — shared HTTP middleware (`MetricsMiddleware`) that wraps chi handler and records `http_requests_total` + `http_request_duration_seconds`.
2. **`/metrics` endpoint** — use `promhttp.Handler()` mounted on the router. Only on API and orchestrator per OBS-02.
3. **LLM metrics** — instrument in `pkg/llm/router.go` (the Router.Chat method) since it knows model/provider.
4. **Tool dispatch metrics** — instrument in `services/orchestrator/internal/orchestrator/orchestrator.go` around `o.tools.Execute()`.

### Histogram Buckets

Use `prometheus.DefBuckets` for HTTP (0.005 to 10s). For LLM calls, use wider buckets: `{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}` since LLM calls routinely take 2-30s.

---

## 4. Structured JSON Logging (OBS-03)

### Current State

`pkg/logger/logger.go` already uses `slog.NewJSONHandler` and attaches `service`. This is 80% of the way there.

### Required Changes

1. **Add `env` and `version` fields** to every log line:
   - `env`: from `ENV` environment variable (default: `"development"`).
   - `version`: from `VERSION` environment variable or build-time `ldflags` (default: `"dev"`).

2. **Add correlation ID to log context** — see section 5.

3. **Configurable log level** — already exists via `NewWithLevel`, but `New()` hardcodes `LevelInfo`. Add `LOG_LEVEL` env var support.

### Updated API

```go
// pkg/logger/logger.go
type Config struct {
    Service string
    Env     string  // from ENV env var
    Version string  // from VERSION env var or ldflags
    Level   slog.Level
}

func New(cfg Config) *slog.Logger {
    handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.Level})
    return slog.New(handler).With(
        slog.String("service", cfg.Service),
        slog.String("env", cfg.Env),
        slog.String("version", cfg.Version),
    )
}
```

Backward compat: keep `New(service string)` as a convenience wrapper that reads env vars internally. This avoids changing every `cmd/main.go` call site if desired — but changing them is also acceptable since there are only 5 services.

### slog Context Integration

`slog` supports `slog.Logger.WithGroup` and `slog.Logger.With` for adding fields. For per-request fields (like correlation ID), use a custom `slog.Handler` wrapper that extracts values from `context.Context`:

```go
// pkg/logger/context_handler.go
type ContextHandler struct {
    inner slog.Handler
}

func (h *ContextHandler) Handle(ctx context.Context, r slog.Record) error {
    if corrID := CorrelationIDFromContext(ctx); corrID != "" {
        r.AddAttrs(slog.String("correlation_id", corrID))
    }
    return h.inner.Handle(ctx, r)
}
```

This requires using `slog.InfoContext(ctx, ...)` instead of `slog.Info(...)` at call sites where correlation ID matters. Not all call sites need migration — focus on request handlers and tool execution paths.

---

## 5. Correlation ID (OBS-04)

### Design

A correlation ID is a UUID generated at the API boundary that follows a request through all downstream services and NATS messages.

### Generation

- Generated in API middleware when not present in incoming `X-Correlation-ID` header.
- Use `uuid.New().String()` (already a dependency) or `chi/middleware.RequestID` value (already generated but stored in chi's context key).

Decision: **Generate our own** rather than reusing chi's RequestID, because:
1. We need it in a well-known context key accessible across packages.
2. We need to propagate it as `X-Correlation-ID` header to orchestrator.
3. Chi's RequestID uses its own internal context key.

### HTTP Header Propagation

```
Client → API (generates X-Correlation-ID)
    API chat_proxy.go → Orchestrator (forwards X-Correlation-ID header)
```

The correlation ID middleware:
1. Checks `X-Correlation-ID` request header — use if present.
2. Otherwise generates new UUID.
3. Stores in context via `pkg/logger` context key.
4. Sets `X-Correlation-ID` response header (for client debugging).

### NATS Propagation

`a2a.ToolRequest` already has `RequestID string` field. The orchestrator's `natsexec.NATSExecutor.Execute()` must:
1. Extract correlation ID from context.
2. Set it as `ToolRequest.RequestID`.

On the agent side, `a2a.Agent.handle()` must:
1. Read `ToolRequest.RequestID`.
2. Store it in context passed to the handler.
3. Use `slog.InfoContext(ctx, ...)` so correlation ID appears in agent logs.

### Context Key Location

Place correlation ID context helpers in `pkg/logger/` alongside the ContextHandler:

```go
// pkg/logger/correlation.go
type ctxKey int
const correlationIDKey ctxKey = iota

func WithCorrelationID(ctx context.Context, id string) context.Context
func CorrelationIDFromContext(ctx context.Context) string
```

This keeps the logger package self-contained — it owns both the context key and the handler that reads it.

---

## 6. Integration Points per Service

### pkg/ (shared)

| File | Change |
|------|--------|
| `pkg/logger/logger.go` | Add `env`, `version` fields; `LOG_LEVEL` env support |
| `pkg/logger/context_handler.go` | New — ContextHandler for correlation ID injection |
| `pkg/logger/correlation.go` | New — context helpers for correlation ID |
| `pkg/health/health.go` | New — Checker type with LiveHandler/ReadyHandler |
| `pkg/metrics/middleware.go` | New — Prometheus HTTP middleware |
| `pkg/go.mod` | Add `prometheus/client_golang` dependency |

### services/api/

| File | Change |
|------|--------|
| `cmd/main.go` | Update logger init; create Checker with PG/Mongo/Redis checks; pass to router |
| `internal/router/router.go` | Mount `/health/live`, `/health/ready`, `/metrics`; add correlation ID middleware; add metrics middleware |
| `internal/middleware/correlation.go` | New — correlation ID middleware (generate/extract, store in context, set response header) |
| `internal/handler/chat_proxy.go` | Forward `X-Correlation-ID` header to orchestrator |

### services/orchestrator/

| File | Change |
|------|--------|
| `cmd/main.go` | Update logger init; create Checker with NATS check; mount health + metrics endpoints; add correlation ID + metrics middleware |
| `internal/handler/chat.go` | Extract correlation ID from request header, store in context |
| `internal/natsexec/executor.go` | Read correlation ID from context, populate `ToolRequest.RequestID` |
| `internal/orchestrator/orchestrator.go` | Add tool dispatch metrics (counter + histogram) |

### services/agent-telegram/

| File | Change |
|------|--------|
| `cmd/main.go` | Add logger init with `logger.New`; add minimal HTTP server for health endpoints; create Checker with NATS check |

### services/agent-vk/

| File | Change |
|------|--------|
| `cmd/main.go` | Same as telegram agent |

### services/agent-yandex-business/

| File | Change |
|------|--------|
| `cmd/main.go` | Same as telegram agent |

### pkg/a2a/

| File | Change |
|------|--------|
| `agent.go` | In `handle()`, extract `ToolRequest.RequestID` and store correlation ID in context before calling handler |

---

## 7. Dependencies and Ordering

### New Go Dependencies

| Package | Version | Used By |
|---------|---------|---------|
| `github.com/prometheus/client_golang` | v1.22+ | `pkg/metrics/`, API, orchestrator |

No other new external dependencies. All other work uses stdlib (`log/slog`, `net/http`, `context`) and existing deps (`uuid`).

### Implementation Order

The four plans are naturally ordered by dependency:

1. **PLAN-5.3: pkg/logger (OBS-03)** — Foundation. Must be done first because all other plans emit logs.
   - Update `pkg/logger` with env/version fields, ContextHandler, correlation context helpers.
   - Update all `cmd/main.go` files to use new logger config.

2. **PLAN-5.4: Correlation ID (OBS-04)** — Depends on logger context infrastructure from 5.3.
   - Add correlation middleware to API.
   - Wire forwarding in chat_proxy.go.
   - Wire into natsexec executor.
   - Wire into a2a.Agent.handle().

3. **PLAN-5.1: Health checks (OBS-01)** — Independent of logging/correlation, but benefits from structured logging being in place.
   - Create `pkg/health/`.
   - Add health endpoints to all 5 services.
   - Add minimal HTTP server to agent services.

4. **PLAN-5.2: Prometheus metrics (OBS-02)** — Benefits from correlation ID middleware being in place (same middleware chain position).
   - Create `pkg/metrics/`.
   - Add metrics middleware + `/metrics` endpoint to API and orchestrator.
   - Instrument LLM router and tool dispatch.

Plans 5.1 and 5.2 are independent of each other and could be done in parallel, but sequencing them avoids merge conflicts in `router.go` and `cmd/main.go`.

---

## 8. Validation Architecture

### OBS-01: Health Checks
- **Test**: HTTP GET `/health/live` returns 200 for all services.
- **Test**: HTTP GET `/health/ready` returns 200 when deps are available.
- **Test**: HTTP GET `/health/ready` returns 503 with failing check names when a dep is down (mock the check function).
- **Manual**: Deploy and hit endpoints from k8s probe config or curl.

### OBS-02: Prometheus Metrics
- **Test**: HTTP GET `/metrics` returns 200 with `text/plain` content containing `http_requests_total`.
- **Test**: After sending a request through the API, `/metrics` shows incremented counters.
- **Test**: Path label uses chi route pattern, not actual URL.
- **Manual**: Scrape with Prometheus and verify in query UI.

### OBS-03: Structured JSON Logging
- **Test**: Capture stdout from logger, parse JSON, verify `service`, `env`, `version` fields are present.
- **Test**: With ContextHandler, verify `correlation_id` appears when context has one.
- **Visual**: Start any service and confirm log output is valid JSON with all required fields.

### OBS-04: Correlation IDs
- **Test**: Send request to API without `X-Correlation-ID` — response contains `X-Correlation-ID` header with a UUID.
- **Test**: Send request to API with `X-Correlation-ID: test-123` — response echoes `test-123`.
- **Test**: In natsexec, verify `ToolRequest.RequestID` is populated from context.
- **Test**: In a2a.Agent, verify handler receives context with correlation ID from `ToolRequest.RequestID`.
- **Integration**: Send chat request, check API logs, orchestrator logs, and agent logs all contain the same correlation ID.

---

## RESEARCH COMPLETE
