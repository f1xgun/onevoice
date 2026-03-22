# Requirements: OneVoice — Observability & Debugging Milestone

**Defined:** 2026-03-22
**Core Value:** Business owners can manage their digital presence across multiple platforms through a single conversational interface

## v1.1 Requirements

Requirements for this milestone. Each maps to roadmap phases.

### Logging Infrastructure

- [ ] **LOG-01**: Grafana + Loki + Promtail добавлены в docker-compose, логи всех сервисов агрегируются в Loki
- [ ] **LOG-02**: Grafana дашборд для поиска логов по correlation_id — один запрос виден через все сервисы
- [ ] **LOG-03**: Prometheus datasource в Grafana с дашбордом: HTTP latency, error rate, tool dispatch metrics

### Backend Logging Gaps

- [ ] **BLG-01**: SSE parsing ошибки логируются в chat_proxy + scanner.Err() обрабатывается после event loop
- [ ] **BLG-02**: Correlation ID сохраняется в persistence-контекстах (не теряется через context.Background)
- [ ] **BLG-03**: NATS tool request/response логируется с timing, tool name, business_id, correlation_id в a2a.Agent
- [ ] **BLG-04**: Platform sync результаты создают AgentTask записи со статусом done/error
- [ ] **BLG-05**: SSE write ошибки логируются в оркестраторе (fmt.Fprintf failures)
- [ ] **BLG-06**: Rate limiter использует request context (r.Context()) вместо context.Background()

### Frontend Logging

- [ ] **FLG-01**: Фронтенд логирует действия пользователя (навигация, клики ключевых кнопок) и отправляет на API endpoint
- [ ] **FLG-02**: API endpoint POST /api/v1/telemetry принимает фронтенд-логи с correlation_id, пишет в stdout (подхватывается Loki)
- [ ] **FLG-03**: Ошибки API-запросов на фронтенде логируются с X-Correlation-ID из response headers для сопоставления с бэком

## v2 Requirements

Deferred to future milestone.

- **OBS-05**: OpenTelemetry distributed tracing (spans) across NATS messages
- **OBS-06**: Alerting rules в Grafana на критические ошибки
- **VKF-01**: VK read-операции через proper VK API service key

## Out of Scope

| Feature | Reason |
|---------|--------|
| ELK Stack (Elasticsearch) | Loki + Grafana легче, достаточно для текущего масштаба |
| Jaeger/Zipkin tracing | Correlation ID + Loki достаточно, OTel deferred |
| Real User Monitoring (RUM) | Фронтенд-логов действий достаточно |
| APM (Application Performance Monitoring) | Prometheus + Grafana покрывает потребности |

## Traceability

| Requirement | Phase | Status |
|-------------|-------|--------|
| LOG-01 | TBD | Pending |
| LOG-02 | TBD | Pending |
| LOG-03 | TBD | Pending |
| BLG-01 | TBD | Pending |
| BLG-02 | TBD | Pending |
| BLG-03 | TBD | Pending |
| BLG-04 | TBD | Pending |
| BLG-05 | TBD | Pending |
| BLG-06 | TBD | Pending |
| FLG-01 | TBD | Pending |
| FLG-02 | TBD | Pending |
| FLG-03 | TBD | Pending |

**Coverage:**
- v1.1 requirements: 12 total
- Mapped to phases: 0
- Unmapped: 12

---
*Requirements defined: 2026-03-22*
