# Milestones

## v1.1 Observability & Debugging (Shipped: 2026-03-22)

**Phases completed:** 3 phases, 6 plans, 12 tasks

**Key accomplishments:**

- Context-aware slog.ErrorContext for all chat_proxy errors and per-operation Telegram sync AgentTask records
- SSE write failures now logged with correlation_id and event type; NATS tool dispatch logs timing, tool name, business_id on all code paths
- Loki + Promtail + Prometheus + Grafana deployed as docker-compose overlay with auto-provisioned datasources
- Two provisioned Grafana dashboards: Request Trace (Loki correlation_id log search) and Metrics Overview (Prometheus HTTP/tool/LLM panels)
- POST /api/v1/telemetry handler with slog structured logging, frontend batched telemetry client with sendBeacon fallback, and X-Correlation-ID error capture in Axios interceptor
- Page navigation, chat send, and key button click telemetry wired into frontend via trackEvent/trackClick with zero UI impact

---

## v1.0 Hardening (Shipped: 2026-03-19)

**Phases completed:** 6 phases, 24 plans, 55 tasks

**Key accomplishments:**

- (none recorded)

---
