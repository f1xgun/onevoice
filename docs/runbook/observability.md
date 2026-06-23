# Observability Runbook

Operator-facing guide to the OneVoice metrics, logs, and alerting stack.

Audience: a single operator with tailnet access. Familiarity with `docker compose`, `curl`, and the Grafana UI is assumed. For tailnet setup itself see `docs/runbook-observability-access.md`.

---

## 1. Dashboard access

The full observability stack (Grafana, Prometheus, Loki, Promtail, Alertmanager) runs on the docker-compose `onevoice-network` bridge with **no public port mappings**. Access requires Tailscale.

| Service | URL (from a tailnet host) | Default credentials |
|---------|---------------------------|---------------------|
| Grafana | `http://<obs-host>:3000` | `admin` / value of `GF_SECURITY_ADMIN_PASSWORD` in `.env.prod` |
| Prometheus | `http://<obs-host>:9090` | none (tailnet ACL is the auth boundary) |
| Alertmanager | `http://<obs-host>:9093` | none (tailnet ACL is the auth boundary) |
| Loki | `http://<obs-host>:3100` | none |

`<obs-host>` is the production host's MagicDNS name (e.g. `onevoice-prod`). See `docs/runbook-observability-access.md` for the one-time Tailscale onboarding.

The primary dashboard is **OneVoice Health** (uid `onevoice-health`), located in Grafana → Dashboards → Browse. Drill-down dashboards live alongside it:

- `metrics-overview` — HTTP / Tool / LLM detail panels
- `request-trace` — per-request trace inspection

### Verifying scrape health from CLI

```bash
curl -s 'http://<obs-host>:9090/api/v1/query?query=up' \
  | jq '.data.result[] | {job: .metric.job, up: .value[1]}'
```

Expected: 7 entries (api, orchestrator, agent-telegram, agent-vk, agent-yandex-business, agent-google-business, pushgateway), all with `"up": "1"`. A `"0"` here means Prometheus could not reach that target — start with `docker compose ps` on the host.

### Smoke-testing /metrics endpoints

```bash
HOST=<obs-host> bash scripts/smoke/metrics_endpoints.sh
```

Hits `/metrics` on all 6 service containers and asserts the Prometheus exposition header is present.

---

## 2. Silencing an alert

Alerts page the operator via the dedicated `@onevoice_alerts_bot` Telegram bot. When the operator is investigating a known issue and wants to mute notifications without disabling the rule:

### Via Alertmanager UI (recommended)

1. From a tailnet host, open `http://<obs-host>:9093`.
2. Click **Silences** → **New silence**.
3. Matcher: `alertname=<the alert>` (e.g. `alertname=BrowserPoolEvictionRate`).
4. Optional second matcher to narrow scope (e.g. `provider=openrouter` for `LLMProviderHighErrorRate`).
5. Duration: typical 1h investigation window.
6. Add a comment with the incident reference. Anonymous silences are noise — always document.

### Via Alertmanager API (scriptable)

```bash
curl -X POST -H 'Content-Type: application/json' \
  http://<obs-host>:9093/api/v2/silences \
  -d '{
    "matchers": [{"name":"alertname","value":"PgxpoolHighAcquireDuration","isRegex":false}],
    "startsAt": "'"$(date -u +%Y-%m-%dT%H:%M:%SZ)"'",
    "endsAt": "'"$(date -u -v+1H +%Y-%m-%dT%H:%M:%SZ)"'",
    "createdBy": "operator",
    "comment": "investigating slow query — incident #NNN"
  }'
```

(GNU date users substitute `-d '+1 hour'` for the BSD `-v+1H`.)

Silences expire automatically at `endsAt`. To revoke early: same UI, **Expire**.

### When NOT to silence

If a warning has been firing for hours with no business impact, do not silence indefinitely — tune the threshold (see Appendix). Indefinite silences accumulate and obscure real signal.

---

## 3. Telegram bot bootstrap and rotation

The alerting bot is **separate** from the business-facing `agent-telegram` bot. The operator owns it personally and must not reuse `TELEGRAM_BOT_TOKEN` from `.env`.

### Initial setup (one-time)

1. Open Telegram, message `@BotFather`, send `/newbot`.
2. Choose a name (`OneVoice Alerts`) and a username (`onevoice_alerts_bot`).
3. BotFather returns a bot token of the form `123456:ABC-DEF...`. Save it in the ops password manager.
4. From your personal Telegram, send `/start` to the new bot. Without this step, the bot cannot DM you.
5. Retrieve your chat_id (Telegram's stable numeric identifier for your DM):
   ```bash
   curl -s "https://api.telegram.org/bot<TOKEN>/getUpdates" \
     | jq '.result[].message.chat.id'
   ```
6. Write both values to `.env` on the obs host:
   ```
   AM_TELEGRAM_BOT_TOKEN=123456:ABC-DEF...
   AM_TELEGRAM_CHAT_ID=987654321
   ```
7. Restart Alertmanager so the entrypoint re-renders the secret files:
   ```bash
   docker compose -f docker-compose.observability.yml up -d alertmanager
   ```
8. Smoke test end-to-end delivery by injecting a synthetic alert:
   ```bash
   curl -X POST http://<obs-host>:9093/api/v1/alerts \
     -H 'Content-Type: application/json' \
     -d '[{"labels":{"severity":"critical","alertname":"SmokeTest"},"annotations":{"summary":"Smoke test from runbook"}}]'
   ```
   Expected: message arrives in the bot within ~30s with `🔴 CRITICAL` prefix.

### Rotation (if the token leaks or quarterly hygiene)

1. Open `@BotFather` in Telegram, `/mybots` → select the bot → **API Token** → **Revoke current token**.
2. BotFather issues a new token; copy it.
3. Edit `.env` on the obs host, replace `AM_TELEGRAM_BOT_TOKEN`.
4. `docker compose -f docker-compose.observability.yml up -d alertmanager` — the container entrypoint re-renders `/etc/alertmanager/secrets/bot_token` (mode 0600) from the new env var at startup. No image rebuild required.
5. Verify with the smoke-test POST above.
6. Update the password manager entry.

No code change is required for rotation. The alertmanager.yml references the token via `bot_token_file:` (and the chat_id via `chat_id_file:`), and both files are populated from env vars by the container entrypoint. Secrets never appear inline in YAML.

---

## 4. Adding a new panel to OneVoice Health

The dashboard JSON is hand-written at `observability/grafana/dashboards/onevoice-health.json`. Grafana auto-loads any file in this directory via `observability/grafana/provisioning/dashboards/dashboards.yml` — no Grafana API push needed.

### Workflow

1. **Pick the row.** The dashboard has 8 rows in a fixed order: Fleet → HTTP → NATS → LLM → Tool dispatch → RPA → DB pool → Errors. New panels belong in the row whose subsystem they monitor.
2. **Identify the metric name and labels.** Every emitted metric is listed in section 5. If the metric doesn't exist, add a collector to `pkg/metrics/` following the pattern in `pkg/metrics/README.md` (cardinality allowlist + banlist).
3. **Write the PromQL.** Test interactively in Grafana → Explore against the Prometheus datasource before committing.
4. **Edit the JSON.** Find the row's last panel by `gridPos.y` and add a new panel object below it. The dashboard uses a 24-column grid: stat panels are typically `{w:8, h:6}`, timeseries `{w:12, h:8}`. Update neighbouring panels' `y` if you insert in the middle.
5. **Validate JSON:** `python3 -m json.tool observability/grafana/dashboards/onevoice-health.json > /dev/null` must exit 0.
6. **Reload Grafana.** In dev, the provisioner picks up the change within ~10s. In prod, `docker compose -f docker-compose.observability.yml up -d grafana` forces a re-provision.

### Anti-patterns

- Avoid panels with >10 time-series (visual noise). Aggregate via `sum by (label)`.
- Don't put the raw metric name in the panel title — title is operator-readable English, the query is the truth.
- Don't use `${var}` Grafana templating for metric names — write each query literally so `grep` for a metric name finds every panel that uses it.

---

## 5. Metric glossary

Maps `pkg/metrics/*.go` collectors to the Prometheus metric names that dashboards and alerts query. Use this when reading a panel or writing a new alert.

| Collector file | Metric (Prometheus name) | Labels | What it measures |
|----------------|--------------------------|--------|------------------|
| pkg/metrics/middleware.go | `http_requests_total` | job, method, path, status | HTTP request count (api, orchestrator) |
| pkg/metrics/middleware.go | `http_request_duration_seconds` (histogram) | job, method, path | HTTP latency |
| pkg/metrics/sse.go | `sse_concurrency_inflight` (gauge) | — | Live SSE streams |
| pkg/metrics/sse.go | `sse_concurrency_blocked_total` | — | SSE rejections due to back-pressure |
| pkg/metrics/sse.go | `sse_concurrency_rollback_failed_total` | — | SSE cleanup failures |
| pkg/metrics/sse.go | `orchestrator_streams_inflight` (gauge) | — | In-flight orchestrator SSE streams holding a global concurrency slot (process-wide cap, distinct from the per-user `sse_concurrency_*` on the api) |
| pkg/metrics/sse.go | `orchestrator_streams_rejected_total` | — | Orchestrator SSE requests rejected with 503 because the global stream cap (`MAX_CONCURRENT_STREAMS`) was full |
| pkg/metrics/llm.go | `llm_requests_total` | model, provider, status | LLM call count |
| pkg/metrics/llm.go | `llm_request_duration_seconds` (histogram) | model, provider | Full request duration |
| pkg/metrics/llm.go | `llm_first_token_latency_seconds` (histogram) | model, provider | Time-to-first-token (stream) |
| pkg/metrics/llm.go | `llm_cache_*` | (existing) | Anthropic prompt-cache hit ratio |
| pkg/metrics/tools.go | `tool_dispatch_total` | tool, agent, status | Tool calls dispatched |
| pkg/metrics/tools.go | `tool_dispatch_duration_seconds` (histogram) | tool, agent | Tool call duration |
| pkg/metrics/yandex.go | `browserpool_contexts` (gauge) | — | Live Playwright contexts in pool |
| pkg/metrics/yandex.go | `browserpool_evictions_total` | reason | LRU evictions from the pool |
| pkg/metrics/yandex.go | `rpa_step_duration_seconds` (histogram) | step, result | Per-step RPA latency (listCompanies, getInfo, getReviews, replyReview, createPost, updateHours, updateInfo, uploadPhoto) |
| pkg/metrics/nats.go | `nats_publish_total` | subject, result | NATS publish count (a2a transport) |
| pkg/metrics/nats.go | `nats_publish_duration_seconds` (histogram) | subject | NATS publish latency |
| pkg/metrics/nats.go | `nats_handler_duration_seconds` (histogram) | subject, result | NATS subscribe-side handler latency |
| pkg/metrics/dbpool.go | `pgxpool_acquire_count_total` | — | Cumulative successful pgxpool acquires |
| pkg/metrics/dbpool.go | `pgxpool_acquire_duration_seconds_total` | — | Cumulative acquire duration; **mean = rate(_total) / rate(_count_total)** (not a histogram) |
| pkg/metrics/dbpool.go | `pgxpool_acquired_conns`, `pgxpool_idle_conns`, `pgxpool_max_conns` | — | pgxpool capacity gauges |
| pkg/metrics/dbpool.go | `mongo_pool_in_use` (gauge) | — | Live mongo checkouts |
| pkg/metrics/dbpool.go | `mongo_pool_checkout_duration_seconds` (histogram) | — | Time spent waiting for a mongo connection |
| pkg/metrics/dbpool.go | `mongo_op_duration_seconds` (histogram) | op, result | Mongo command duration (op allowlisted: find, insert, update, delete, aggregate, count, findAndModify, other) |
| pkg/metrics/llm_billing.go | `llm_billing_*` | (existing) | Cost accounting |
| (Prometheus built-in) | `up` | job, instance | 1 if last scrape succeeded |
| (pushgateway, Phase 23) | `backup_last_success_timestamp` | (preserved labels) | Unix epoch of last backup success |
| pkg/metrics/sweepers.go | `sweeper_runs_total` | sweeper, result | Background deletion/warning sweeper passes (sweeper: account_hard_delete, business_hard_delete, deletion_warning; result: ok, error) |
| pkg/metrics/sweepers.go | `sweeper_items_processed_total` | sweeper | Items acted upon (users/orgs hard-deleted, T-7 warning mails enqueued) |
| pkg/metrics/sweepers.go | `sweeper_last_success_timestamp` (gauge) | sweeper | Unix epoch of last error-free sweeper pass; seeded at startup so a never-succeeding sweeper still ages past the `*SweeperStalled` threshold |

### Label discipline

See `pkg/metrics/README.md` for the authoritative allowlist + banlist. Never use `business_id`, `user_id`, `email`, `conversation_id`, or `request_id` as metric labels — they explode time-series cardinality. New collectors must follow the patterns documented there.

### Note on label value conventions

LLM and tool collectors emit `status="success"` and `status="error"`. The `LLMProviderHighErrorRate` alert queries the numerator as `status!="ok"` (no value labelled `"ok"` is ever emitted today, so this matches both `"success"` and `"error"`, which is functionally identical to `status="error"`). Dashboards use exact `status="error"` for clarity. When adding new alerts, prefer the exact match form: `status="error"`.

---

## 6. Incident response flowchart

```
🔴 CRITICAL alert arrives in Telegram
  │
  ├── ServiceDown                              (fleet.yml; up{} == 0 for 2m)
  │     → docker compose ps                    — which container is missing?
  │     → docker compose logs <service>        — last 100 lines tell the story
  │     → restart if transient:                  docker compose up -d <service>
  │     → if persistent: env vars, dependencies (postgres, mongo, nats), tailnet reachability
  │
  ├── SSEHigh5xxRatio                          (http.yml; orchestrator /chat 5xx > 5% for 5m)
  │     → Grafana → OneVoice Health → HTTP row
  │     → Loki → {service="orchestrator",level="error"}
  │     → likely cause: LLM provider down (cross-check LLMProviderHighErrorRate)
  │                     or NATS broker degraded (cross-check NATSPublishErrorRate)
  │
  ├── LLMProviderHighErrorRate                 (llm.yml; > 10% non-ok per provider for 5m)
  │     → Grafana → OneVoice Health → LLM row → check provider label
  │     → provider status pages: openrouter.ai/status, status.anthropic.com, status.openai.com
  │     → mitigation: temporary LLM_MODEL flip in .env + docker compose up -d orchestrator
  │
  ├── NATSPublishErrorRate                     (nats.yml; > 1/s for 2m)
  │     → docker compose logs nats             — broker crash, OOM, file-descriptor exhaust?
  │     → restart: docker compose up -d nats
  │     → if persistent: check NATS JetStream storage (disk full?) and tailnet partition
  │
  ├── BackupStale                              (backup.yml; no success in 26h)
  │     → docs/runbook-restore.md              — verify restic chain integrity
  │     → check pushgateway for stuck job:     curl http://<obs-host>:9091/metrics | grep backup_
  │     → check the backup cron container's logs for the most recent run
  │
🟡 WARNING alert arrives in Telegram
  │
  ├── BrowserPoolEvictionRate                  (rpa.yml; > 1 eviction/min for 5m)
  │     → not page-out urgent; investigate during business hours
  │     → confirm cause via Grafana → RPA row → evictions by reason
  │     → tune YANDEX_POOL_MAX (or BROWSER_POOL_MAX_CONTEXTS) in .env if sustained
  │     → if reason=memory, the host may be undersized
  │
  └── PgxpoolHighAcquireDuration               (dbpool.yml; mean > 500ms for 5m)
        → not page-out urgent; investigate during business hours
        → Grafana → DB pool row → look at acquired_conns vs max_conns
        → if pool saturated: tune PG_POOL_MAX / PG_POOL_MIN in .env
        → if pool fine but acquire still slow: check pg_stat_activity for long-running queries
```

### When to silence vs investigate vs tune

- **Silence** (≤ 24h, with a comment): known issue under active investigation, or post-deploy noise during the first 30 min.
- **Investigate immediately**: any critical that is not a known-known.
- **Tune the threshold** (don't silence repeatedly): if a rule fires regularly with no real incident, edit `observability/prometheus/alerts/*.yml`, open a PR. CI will gate the YAML.

### Resolved-alert behaviour

Alertmanager is configured with `send_resolved: true`. When an alert stops firing, Telegram receives a follow-up message with `[RESOLVED]` prefix. If a critical fires and then resolves within `group_wait` (10s), only the resolved notification arrives.

---

## Appendix: tuning alert thresholds

Current starting values (intentionally conservative for v1.4 beta):

| Alert | Threshold | Window | Severity | File |
|-------|-----------|--------|----------|------|
| SSEHigh5xxRatio | > 5% | 5m | critical | http.yml |
| LLMProviderHighErrorRate | > 10% | 5m | critical | llm.yml |
| NATSPublishErrorRate | > 1/s | 2m | critical | nats.yml |
| ServiceDown | up == 0 | 2m | critical | fleet.yml |
| BackupStale | last success > 26h | 10m | critical | backup.yml |
| BrowserPoolEvictionRate | > 1/min | 5m | warning | rpa.yml |
| PgxpoolHighAcquireDuration | mean > 500ms | 5m | warning | dbpool.yml |

To tune:

1. Edit the `expr:` or `for:` field in the YAML file.
2. Commit and open a PR. CI runs `promtool check rules` on the changed files — bad YAML cannot land.
3. After merge, Prometheus hot-reloads rule files on its next scrape interval (15s); for an immediate reload: `docker compose -f docker-compose.observability.yml up -d prometheus`.

### Adding a new alert

1. Pick (or create) a rule file under `observability/prometheus/alerts/<subsystem>.yml`.
2. Add a rule with `alert:`, `expr:`, `for:`, `labels.severity:` (critical or warning), `annotations.summary:`, `annotations.description:`.
3. The description should be operator-actionable — link to this runbook section or `docs/runbook-restore.md` as appropriate.
4. CI gates the YAML via `promtool check rules`. Locally:
   ```bash
   docker run --rm -v "$PWD/observability:/o" --entrypoint /bin/promtool \
     prom/prometheus:v2.52.0 \
     check rules /o/prometheus/alerts/<file>.yml
   ```

---

## References

- `docker-compose.observability.yml` — full obs stack composition
- `observability/prometheus/prometheus.yml` — scrape config + rule_files glob + alertmanager wiring
- `observability/alertmanager/alertmanager.yml` — receiver and routing
- `observability/grafana/dashboards/onevoice-health.json` — primary dashboard
- `pkg/metrics/README.md` — label cardinality discipline
- `scripts/smoke/metrics_endpoints.sh` — /metrics smoke for all 6 services
- `docs/runbook-observability-access.md` — Tailscale onboarding (prerequisite for accessing this stack)
- `docs/runbook-restore.md` — backup restore procedure (referenced by BackupStale)
- `.env.example` — AM_TELEGRAM_BOT_TOKEN, AM_TELEGRAM_CHAT_ID, GF_SECURITY_ADMIN_PASSWORD
