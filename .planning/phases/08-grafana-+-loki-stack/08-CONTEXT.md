# Phase 8: Grafana + Loki Stack - Context

**Gathered:** 2026-03-22
**Status:** Ready for planning

<domain>
## Phase Boundary

Deploy centralized log aggregation (Loki + Promtail) and metrics dashboards (Grafana + Prometheus) via docker-compose. Provision dashboards as JSON for reproducibility.

</domain>

<decisions>
## Implementation Decisions

### Claude's Discretion
All implementation choices are at Claude's discretion — pure infrastructure phase.

</decisions>

<code_context>
## Existing Code Insights

### Reusable Assets
- `pkg/metrics/middleware.go` — Prometheus HTTP metrics (http_requests_total, http_request_duration_seconds) already exported
- `pkg/metrics/llm.go` — LLM call metrics
- `pkg/metrics/tools.go` — Tool dispatch metrics
- Prometheus `/metrics` endpoint already live on API and orchestrator services (OBS-02 from v1.0)
- All Go services use `log/slog` with JSON handler via `pkg/logger/` — structured logs ready for Loki ingestion

### Established Patterns
- `docker-compose.yml` — main compose file with all services on `onevoice-network`
- Services already output structured JSON logs to stdout
- `correlation_id` field present in all logs (via ContextHandler from Phase 7)
- Nginx reverse proxy at port 80

### Integration Points
- New `docker-compose.observability.yml` should be a separate overlay file (keeps main compose clean)
- Grafana needs to connect to Loki (logs) and Prometheus (metrics) as datasources
- Promtail reads Docker container logs via `/var/lib/docker/containers`
- Prometheus scrapes `/metrics` from api:8080 and orchestrator:8090
- Grafana exposed on a port (e.g., 3001) for developer access

</code_context>

<specifics>
## Specific Ideas

No specific requirements — infrastructure phase.

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope.

</deferred>
