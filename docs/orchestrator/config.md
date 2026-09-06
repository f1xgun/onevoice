# Orchestrator: Configuration

`services/orchestrator/internal/config/config.go` is the single source of
truth for orchestrator boot-time configuration. `Load()` parses environment
variables, applies defaults, and **fails loud** at startup on semantically
invalid combinations (unknown enum values, malformed positive-integer
fields) so a misconfigured deploy refuses to start instead of silently
disabling a guard.

## Public API

- `type Config` — the configuration struct (see *Field reference*).
- `type SelfHostedEndpoint{URL, Model, APIKey}` — one self-hosted LLM
  inference endpoint; collected as a slice on `Config.SelfHostedEndpoints`.
- `Load() (*Config, error)` — read env, validate, return populated struct.
- `(*Config).RedactMongoURI() string` — returns the Mongo URI with
  embedded `user:password` stripped, safe for startup logs. Returns
  `<mongo-uri-redacted>` if the URI fails to parse rather than leaking the
  raw value.

## Field reference

### LLM routing

| Field | Env | Default | Range / Semantic |
|---|---|---|---|
| `LLMModel` | `LLM_MODEL` | — | **Required.** Boot fails if unset. |
| `DraftReplyModel` | `DRAFT_REPLY_MODEL` | falls back to `LLMModel` | Cheap-tier model used by `services/orchestrator/internal/handler/draft_reply.go` for AI-suggested review replies. Fallback resolved at `Load` time (not in the handler) so the redacted startup log records the effective model. Kept separate from the API's `TitlerModel` so the two cheap-tier callsites can be tuned independently without API surface churn. DraftReply doesn't use tools — any chat-completion model is safe. |
| `LLMTier` | `LLM_TIER` | `"free"` | Tier label for `pkg/llm` router quota selection. |

### Server / lifecycle

| Field | Env | Default | Range / Semantic |
|---|---|---|---|
| `Port` | `PORT` | `"8090"` | HTTP listen port. |
| `ShutdownTimeout` | `SHUTDOWN_TIMEOUT` | `30s` | Graceful-shutdown budget. 30s gives in-flight LLM and tool-dispatch requests time to drain before SIGKILL. |
| `HealthCheckTimeout` | `HEALTH_CHECK_TIMEOUT` | `2s` | Per-dep timeout inside `/health/ready`. Checks run concurrently in `pkg/health.ReadyHandler`, so the wall-clock budget is `max` (not sum). Defensive default + clamp so an operator typo (or zero/negative explicit value) can't disable the safety net. Matches the API service's `HealthCheckTimeout` semantics. |
| `MaxConcurrentStreams` | `MAX_CONCURRENT_STREAMS` | `256` | Process-wide cap on simultaneous SSE streams (`POST /chat` + `/chat/{id}/resume`), enforced by `internal/streamlimit`. Over-cap requests get `503 {"error":"stream_capacity_exceeded"}`. A generous aggregate backstop — the API already bounds per-user concurrency (`pkg/ssecounter`); this only sheds load under a pathological total burst. Set `0` (or negative) to disable. Observed via `orchestrator_streams_inflight` + `orchestrator_streams_rejected_total`. |

### Agent loop

| Field | Env | Default | Range / Semantic |
|---|---|---|---|
| `MaxIterations` | `MAX_ITERATIONS` | `10` | Maximum LLM agent-loop iterations per turn. Soft sanity cap on runaway tool loops. |
| `ToolExecTimeout` | `TOOL_EXEC_TIMEOUT` | `180s` | Bounds a single tool call (applied in both `executeOne` and the HITL resume dispatch). Unset falls back to the 180s default, set above the verified Yandex RPA path's internal waits + retry backoff. Set `0` to disable the per-tool deadline and let the request context govern cancellation. |

### Platform tools

| Field | Env | Default | Range / Semantic |
|---|---|---|---|
| `EnableGoogleBusiness` | `ENABLE_GOOGLE_BUSINESS` | `false` | Registers the Google Business tool set with the registry. Off by default: Google Business is unverified and hidden on the integrations UI, so leaving it on would surface its tools as approvable in Settings → Tools for a platform that can never be connected. Telegram / VK / Yandex.Business always register. **Boot error** on a non-boolean value. |

### Cost guards

Documented in `.env.example` and `docs/llm-cost-guards.md`; operators tune
via env.

| Field | Env | Default | Range / Semantic |
|---|---|---|---|
| `ConversationInputCap` | `LLM_CONVERSATION_INPUT_CAP` | `50000` | Per-conversation cumulative input token budget. `0` disables this axis. Parsed defensively: parse error keeps the default. |
| `ConversationOutputCap` | `LLM_CONVERSATION_OUTPUT_CAP` | `10000` | Per-conversation cumulative output token budget. `0` disables this axis. Same defensive parse. |
| `FreeTierDailySpendUSD` | `LLM_FREE_TIER_DAILY_SPEND_USD` | `0` (compiled default wins) | Overrides `DefaultTierLimits["free"].DailySpendUSD` at wire time. `0` keeps the compiled default; `-1` disables the gate (unlimited); positive sets the dollar cap. |
| `RedisDownPolicy` | `LLM_RATELIMIT_ON_REDIS_DOWN` | `"block"` | Behavior when Redis fails. **Hard boot error** if value is not exactly `"block"` or `"local_fallback"` — refuses to start a misconfigured deploy. `block` = fail-closed; `local_fallback` = in-process bucket. |
| `LocalFallbackRequestsPerHour` | `LLM_LOCAL_FALLBACK_REQUESTS_PER_HOUR` | `2000` | Bucket rate when policy is `local_fallback`. Roughly matches the "$10/h" ceiling at the v1.4 average per-request cost (~$0.005). **Boot error** on non-integer input. **Boot error** when `RedisDownPolicy == local_fallback` and value `<= 0` (required field for that policy). |
| `LocalFallbackWindow` | `LLM_LOCAL_FALLBACK_WINDOW` | `30s` | How long the limiter consults the in-process bucket after a Redis failure before re-probing Redis. |

### LLM providers

| Field | Env | Default | Range / Semantic |
|---|---|---|---|
| `OpenRouterAPIKey` | `OPENROUTER_API_KEY` | `""` | At least one provider key must be set for the LLM Router to boot. |
| `OpenAIAPIKey` | `OPENAI_API_KEY` | `""` | — |
| `AnthropicAPIKey` | `ANTHROPIC_API_KEY` | `""` | — |
| `SelfHostedEndpoints` | `SELF_HOSTED_N_URL`, `SELF_HOSTED_N_MODEL`, `SELF_HOSTED_N_API_KEY` | `nil` | Indexed env scan; see *Self-hosted endpoints*. |

### Personal-data residency (152-FZ transborder guard)

| Field | Env | Default | Range / Semantic |
|---|---|---|---|
| `AllowTransborderLLM` | `ALLOW_TRANSBORDER_LLM` | `false` | When `false` (default) the orchestrator redacts personal data from every outbound LLM request before it can reach a provider running outside Russia. `cmd/main.go` wires `Options.RedactOutboundPDn = !AllowTransborderLLM`, so the *safe* posture is the zero value. In production (`APP_ENV=production`) the same flag gates the LLM residency check in `wire.LLMRouter`: with the flag `false`, boot is refused when any hosted key (`OPENROUTER_API_KEY` / `OPENAI_API_KEY` / `ANTHROPIC_API_KEY`) is set or when `LLM_MODEL` / `DRAFT_REPLY_MODEL` is not served by a `SELF_HOSTED_N_*` endpoint (`llm.EnforceResidency`). **Boot error** on a non-boolean value. |

The redaction chokepoint lives in `internal/orchestrator/redact.go` (`applyOutboundRedaction`), invoked in `stepRun` just before `o.llm.Chat` — so it covers both the fresh `Run` and the HITL `Resume` path (both route through `stepRun`). It scrubs, via `pkg/security.RedactPII`:

- conversation **history** message content (untrusted review/DM bodies the LLM is fed back),
- **tool-call arguments and tool-result** content,
- the **per-business** prompt block (block 2) — third-party phone/email/card patterns.

It never touches block 1 (the locale-fixed platform prefix that anchors the provider cache prefix), and it works on a copy so the persisted HITL pause snapshot keeps the original, un-redacted bytes for the approval card and audit. `RedactPII` matches email / RU phone / IBAN / RU passport / INN / Luhn-valid card numbers; it does **not** recognize free-form physical addresses or person names, so the business `Адрес` line is preserved (lower-risk owner data, frequently load-bearing for the assistant). Set `ALLOW_TRANSBORDER_LLM=true` only with a documented legal basis for transborder transfer, or when inference is pinned to RU / self-hosted endpoints.

#### Business-owned contact allowlist

The scrub can preserve the business's **own registered contacts**. `BusinessContext.Phone` and `BusinessContext.Website` supply candidate allowlist values, but only complete phone or email entries grant exemptions; website URLs and domains do not. They are entered by the owner, rendered on the business's public profile, and routinely load-bearing (a post that must carry the order line, a review reply that points a customer at the shop) — redacting them breaks the product without protecting anyone's personal data. Third-party contacts anywhere else, including in the same sentence, are still redacted.

`redact.go:businessContactAllowlist` derives the list from the `RunRequest`, `RunState.PDnAllowlist` carries it through the loop, and `pkg/security.RedactPIIExcept` applies it on every scrubbed surface (history content, tool-call arguments, block 2). Email matching ignores case but preserves all punctuation. Phone matching requires a complete phone entry, compares digits, and folds the Russian trunk prefix `8` onto `7`, so `+7 (843) 555-12-34`, `8 843 555-12-34` and `+78435551234` are one value. The allowlist is persisted on the HITL pause snapshot (`pdn_allowlist`, `omitempty`) so a resumed turn scrubs exactly like the paused one; legacy snapshots without the field simply fall back to allowlist-free redaction.

The `/internal/draft-reply` ingress has no registered contact fields on its request body, so it redacts without an allowlist.

The same `ALLOW_TRANSBORDER_LLM` knob also gates the second outbound LLM ingress — the standalone `/internal/draft-reply` handler (`internal/handler/draft_reply.go`), which sends review text + few-shot examples + the business block to the LLM. `wire/handlers.go` wires `NewDraftReplyHandler(..., !cfg.AllowTransborderLLM)`, so the flag is all-or-nothing across both ingresses: when `false` (default) the handler scrubs every outbound message via `redactDraftMessages` (the same `RedactPII`) before the provider call. The third ingress, the API auto-titler, redacts unconditionally regardless of this flag (see `docs/services/titler.md`).

### Storage / message bus

| Field | Env | Default | Range / Semantic |
|---|---|---|---|
| `NATSUrl` | `NATS_URL` | `"nats://localhost:4222"` | NATS server URL for tool dispatch. |
| `MongoURI` | `MONGO_URI` | `"mongodb://localhost:27017"` | The orchestrator writes `pending_tool_calls` batches at pause time, so it needs its own Mongo connection (avoids a circular dependency where orchestrator → API → orchestrator). Defaults match the API service so dev setups that only set one `MONGO_URI` continue to work. |
| `MongoDB` | `MONGO_DB` | `"onevoice"` | Database name on the same client. |
| `RedisURL` | `REDIS_URL` | `""` | Rate-limiter per-minute / per-month counters. Empty disables the rate-limiter wiring at boot. |

### Internal API

| Field | Env | Default | Range / Semantic |
|---|---|---|---|
| `APIInternalURL` | `API_INTERNAL_URL` | `"https://api:8443"` | Base URL of the API service's mTLS-protected internal `:8443` listener. `pkg/billingclient` is wired against it for the orchestrator → api billing POST hop. **Must be HTTPS** — the mTLS substrate requires it on this endpoint. Default matches the docker-compose service DNS. |

## Validation rules (fail-loud at `Load`)

- `LLM_MODEL` unset → `LLM_MODEL is required`.
- `LLM_RATELIMIT_ON_REDIS_DOWN` set to anything other than `"block"` /
  `"local_fallback"` → returns error naming the accepted values.
- `LLM_LOCAL_FALLBACK_REQUESTS_PER_HOUR` set to a non-integer →
  `must be a positive integer`.
- `ALLOW_TRANSBORDER_LLM` set to a non-boolean → `must be a boolean`.
- `LLM_RATELIMIT_ON_REDIS_DOWN=local_fallback` with
  `LLM_LOCAL_FALLBACK_REQUESTS_PER_HOUR <= 0` →
  `must be > 0 when ... =local_fallback`.

Everything else is parse-defensive: an unparseable value keeps the default
(may log a warning at the call site).

## Self-hosted endpoints

`parseIndexedEndpoints()` scans `SELF_HOSTED_N_URL` / `_MODEL` /
`_API_KEY` for `N = 0, 1, 2, …` and stops at the first missing
`_URL`. Entries without a `_MODEL` are skipped silently. The empty result
is a `nil` slice; the LLM Router treats it as "no self-hosted provider
chain".

## `RedactMongoURI` semantics

Supported forms:

- `mongodb://user:pass@host[:port]/db`
- `mongodb+srv://user:pass@host/db`

Algorithm: locate the `://` scheme separator; locate the first `@`
**before** any `/` in the authority; replace the user-info segment with
`***:***`. If no `@` appears before the path, the URI has no user-info
segment and is returned verbatim (safe). If the scheme is missing entirely,
returns the safe placeholder `<mongo-uri-redacted>` rather than leaking
the raw value.

Empty `MongoURI` returns `""`.

## `getEnv` helper

`getEnv(key, defaultValue)` returns `defaultValue` when the env var is
absent **or** the empty string. Used for fields where the empty-string
case is semantically identical to "unset" — most string-typed fields
above. Numeric / duration fields don't use it because their parse paths
have their own zero-handling.

## Cross-references

- `.env.example` — runtime documentation of every env var.
- `docs/llm-cost-guards.md` — narrative on the cost-guard knobs.
- `services/api/internal/config/config.go` — sibling config; `TitlerModel`
  fallback pattern mirrored here for `DraftReplyModel`;
  `HealthCheckTimeout` semantics shared.
- `pkg/health` — `ReadyHandler` (concurrent dep ping pattern).
- `pkg/llm` — Router, tier limits, rate-limiter sentinels.
- `pkg/billingclient` — orchestrator → api mTLS billing hop.
