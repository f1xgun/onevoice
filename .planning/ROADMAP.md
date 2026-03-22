# Roadmap: OneVoice

## Milestones

- ✅ **v1.0 Hardening** — Phases 1-6 (shipped 2026-03-20) — [archive](milestones/v1.0-ROADMAP.md)
- 🔄 **v1.1 Observability & Debugging** — Phases 7-9

## Phases

<details>
<summary>✅ v1.0 Hardening (Phases 1-6) — SHIPPED 2026-03-20</summary>

- [x] Phase 1: Security Foundation (4/4 plans) — completed 2026-03-15
- [x] Phase 2: Reliability Foundation (4/4 plans) — completed 2026-03-16
- [x] Phase 3: VK Agent Completion (5/5 plans) — completed 2026-03-19
- [x] Phase 4: Yandex.Business Agent (5/5 plans) — completed 2026-03-19
- [x] Phase 5: Observability (4/4 plans) — completed 2026-03-20
- [x] Phase 6: Testing Completion (2/2 plans) — completed 2026-03-20

</details>

### v1.1 Observability & Debugging

#### Phase 7: Backend Logging Gaps

**Goal:** Close all 6 backend logging gaps identified in v1.0 audit — silent errors, context loss, missing logs.

**Requirements:** BLG-01, BLG-02, BLG-03, BLG-04, BLG-05, BLG-06

**Plans:** 2 plans

Plans:
- [ ] 07-01-PLAN.md — API service: context-aware logging in chat_proxy, per-op sync tasks, rate limiter confirmation
- [ ] 07-02-PLAN.md — Orchestrator service: SSE write error logging, NATS tool dispatch timing

**Success Criteria:**
1. SSE parsing errors in `chat_proxy` are logged with correlation_id; `scanner.Err()` is checked after the event loop (BLG-01)
2. All `context.Background()` calls in persistence paths are replaced with the request context carrying correlation_id (BLG-02)
3. NATS tool request/response logs include timing (ms), tool name, business_id, and correlation_id in structured JSON fields (BLG-03)
4. Platform sync operations produce AgentTask records with status `done` or `error`, visible in the database (BLG-04)
5. SSE `fmt.Fprintf` failures in orchestrator are logged with the correlation_id and client info (BLG-05)
6. Rate limiter middleware uses `r.Context()` instead of `context.Background()`, preserving request cancellation and correlation (BLG-06)

---

#### Phase 8: Grafana + Loki Stack

**Goal:** Deploy centralized log aggregation and metrics dashboards — one place to search logs by correlation_id across all services.

**Requirements:** LOG-01, LOG-02, LOG-03

**Success Criteria:**
1. `docker-compose.observability.yml` adds Grafana, Loki, and Promtail; `docker compose up` starts the full stack with all service logs flowing into Loki (LOG-01)
2. Grafana has a "Request Trace" dashboard where entering a correlation_id shows logs from API, orchestrator, and agents in chronological order (LOG-02)
3. Prometheus is configured as a Grafana datasource with a dashboard showing HTTP request latency (p50/p95/p99), error rate by service, and tool dispatch duration histogram (LOG-03)
4. All dashboards are provisioned as JSON files (not manual Grafana UI config) so they survive container restarts

---

#### Phase 9: Frontend Telemetry

**Goal:** Add user action logging on the frontend and correlate frontend events with backend traces via correlation_id.

**Requirements:** FLG-01, FLG-02, FLG-03

**Success Criteria:**
1. Frontend sends structured telemetry events (page navigation, key button clicks, chat sends) to the API (FLG-01)
2. `POST /api/v1/telemetry` endpoint accepts an array of frontend events with correlation_id and writes them to stdout in JSON format, picked up by Loki (FLG-02)
3. API error responses on the frontend are logged with the `X-Correlation-ID` from the response header, enabling lookup in Grafana (FLG-03)
4. Telemetry calls are batched/debounced so they do not degrade UI responsiveness

---

## Coverage Validation

| Requirement | Phase | Covered |
|-------------|-------|---------|
| BLG-01 | 7 | ✅ |
| BLG-02 | 7 | ✅ |
| BLG-03 | 7 | ✅ |
| BLG-04 | 7 | ✅ |
| BLG-05 | 7 | ✅ |
| BLG-06 | 7 | ✅ |
| LOG-01 | 8 | ✅ |
| LOG-02 | 8 | ✅ |
| LOG-03 | 8 | ✅ |
| FLG-01 | 9 | ✅ |
| FLG-02 | 9 | ✅ |
| FLG-03 | 9 | ✅ |

**12/12 requirements mapped. 0 unmapped.**

---
*Created: 2026-03-22*
