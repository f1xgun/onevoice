# LLM Cost Guards — Operator Runbook

This document describes the four cost guards that bracket every LLM call in
OneVoice, the env knobs that tune them, and the troubleshooting workflow for
each.

The guards run inside two places:

- **Orchestrator's chat-turn Router** — every user-driven chat goes through
  this path. The rate limiter + token cap + retry policy all apply.
- **API-side Router** (titler, draft-reply) — system tasks that bill against
  the same business. Shares the rate-limiter and daily-spend policy with the
  orchestrator.

## 1. Daily spend cap (per business)

**What it does.** Before each LLM call, the rate limiter reads the running
spend for the business over today's UTC calendar day and compares it to the
configured cap. Reaching the cap surfaces a distinct SSE error so the
frontend can render an "out of budget" state rather than a generic
"rate-limited" one.

**Source of truth.** `services/api/internal/repository.billingRepository.GetDailySpend`
sums `provider_cost_usd + commission_usd` from `usage_logs` for the
(business_id, UTC day). The orchestrator fetches this via
`GET /internal/v1/billing/daily_spend` (mTLS-only). The API-side router
calls the same repository in-process so titler / draft-reply share the same
budget.

**Env knob.**

```
LLM_FREE_TIER_DAILY_SPEND_USD=     # 0=compiled default, -1=unlimited, >0=override
```

The compiled default is `1.00` (free tier). Pro and Basic tier defaults live
in `pkg/llm/DefaultTierLimits` and are not yet env-overridable (per-business
overrides are deferred to the paid-plans milestone).

**Wire shape on the error path.**

```
data: {"type":"error","code":"daily_spend_exceeded","content":"…"}
```

**Troubleshooting.**

| Symptom | Likely cause | Action |
| --- | --- | --- |
| Every chat returns `daily_spend_exceeded` | Cap set too low | Raise `LLM_FREE_TIER_DAILY_SPEND_USD` |
| Counter `llm_daily_spend_blocked_total{tier="free"}` increments but spend < cap | Float boundary case | Counter uses a 1e-9 epsilon; check `usage_logs` for accumulation closer to cap than expected |
| Daily spend lookup returns a transient error | API internal endpoint unreachable | Counter `llm_redis_down_fallback_total{action="misconfigured"}` increments; check mTLS configuration |

## 2. Per-conversation token cap

**What it does.** Inside `services/orchestrator/internal/orchestrator.stepRun`,
the loop accumulates `Usage.InputTokens` and `Usage.OutputTokens` after every
LLM response. When either axis reaches its configured cap, the loop emits a
friendly SSE error and exits without another LLM call.

Tool results count toward the input cap because the next iteration sends them
back as part of the prompt. A single response that overshoots the cap on
iteration 1 trips the same gate (mid-iter overshoot).

The accumulated counts survive a HITL pause: they are persisted in
`modelMessagesSnapshotV2` and rehydrated on Resume, so a paused-and-resumed
turn does not reset the budget to zero.

**Env knobs.**

```
LLM_CONVERSATION_INPUT_CAP=50000   # 0 disables the axis
LLM_CONVERSATION_OUTPUT_CAP=10000  # 0 disables the axis
```

**Wire shape on the error path.**

```
data: {"type":"error","code":"conversation_token_cap","content":"…"}
```

The `content` is a friendly RU/EN string ("This conversation has reached its
token limit. Start a new chat to continue.").

**Troubleshooting.**

| Symptom | Likely cause | Action |
| --- | --- | --- |
| Long chats abort prematurely | Cap too tight | Raise `LLM_CONVERSATION_INPUT_CAP` or `LLM_CONVERSATION_OUTPUT_CAP` |
| Resumed chat exits on the very next turn after approval | The pre-pause turn already accumulated most of the budget | Expected behavior; user must start a new conversation |
| Cap fires on the FIRST iteration of a fresh chat | The supplied prompt already exceeds the cap | Either tighten the system prompt or raise the cap |

## 3. Redis-down policy

**What it does.** When the rate limiter's Redis call fails, the limiter
follows the operator's configured policy:

- **block** (default) — return `ErrRateLimitUnavailable`, surface a
  `rate_limit_unavailable` SSE error. This is the fail-closed default: an
  operator can prefer this whenever silently bypassing the budget gate is
  worse than briefly refusing chat.
- **local_fallback** — consult an in-process token bucket. The bucket is
  per-pod (not per-business), so the cluster-wide spend during an outage is
  bounded by (pods × bucket-rate × outage-duration). When the bucket grants,
  the chat proceeds; when it's exhausted, the user sees `rate_limit_exceeded`
  as if a regular rate-limit gate fired.

**Env knobs.**

```
LLM_RATELIMIT_ON_REDIS_DOWN=block         # or local_fallback
LLM_LOCAL_FALLBACK_REQUESTS_PER_HOUR=2000 # required when policy=local_fallback
LLM_LOCAL_FALLBACK_WINDOW=30s             # consult the local bucket before retrying Redis
```

**Fail-loud validation.** When `LLM_RATELIMIT_ON_REDIS_DOWN=local_fallback`
and `LLM_LOCAL_FALLBACK_REQUESTS_PER_HOUR` is zero or negative, the
orchestrator (and the api) refuse to boot rather than silently disabling
the limiter.

**Mapping "$10/h" to a request rate.** The local fallback is expressed as a
request rate, not a dollar rate, because the in-process bucket has no
visibility into per-call cost. The rough conversion:

```
requests_per_hour ≈ (target_dollars_per_hour) / (average_cost_per_request_usd)
```

At v1.4's ~$0.005 average per-request cost, $10/h ≈ 2000 req/h. Tune by
sampling your `usage_logs.provider_cost_usd` over a typical hour.

**Troubleshooting.**

| Symptom | Likely cause | Action |
| --- | --- | --- |
| Whole platform refuses chat during a Redis blip | Default block policy + Redis down | Flip `LLM_RATELIMIT_ON_REDIS_DOWN=local_fallback`, restart |
| Counter `llm_redis_down_fallback_total{action="misconfigured"}` increments | local_fallback with no bucket wired, or daily-spend lookup itself failed | Check env vars and the internal billing endpoint |
| Bucket exhausts immediately under load | Rate too low for traffic | Raise `LLM_LOCAL_FALLBACK_REQUESTS_PER_HOUR` |

## 4. Prometheus counters

The four cost-guard counters live in `pkg/metrics/llm.go`:

| Counter | Labels | When it fires |
| --- | --- | --- |
| `llm_daily_spend_blocked_total` | `tier` | Daily-spend gate refused a request |
| `llm_conversation_cap_hit_total` | `axis` (`input` or `output`) | Token cap exhausted on the labeled axis |
| `llm_redis_down_fallback_total` | `action` (`block` / `fallback` / `fallback_blocked` / `misconfigured`) | Redis call failed; label records which policy branch executed |
| `llm_router_retry_total` | `result` × `attempt` | Router retry-once outcome (wired by the next plan in this phase) |

Grafana alert ideas:

- Rate-of-change > 0 on `llm_daily_spend_blocked_total` over 5 min → page
  the business owner.
- Rate-of-change > 0 on `llm_redis_down_fallback_total{action="block"}` over
  1 min → page the SRE: the chat platform is wedged.
- Histogram of `llm_conversation_cap_hit_total{axis="input"}` over a day →
  tune the input cap.
