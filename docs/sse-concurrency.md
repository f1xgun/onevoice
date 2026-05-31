# SSE Concurrency Cap

Operator runbook for the per-user SSE concurrency cap enforced by the api service.

## 1. What the cap does

`services/api/internal/handler/chat_proxy.go::Chat` rejects an incoming chat request when the same `user_id` already has `SSE_MAX_PER_USER` in-flight SSE streams. The counter brackets the handler body around the orchestrator stream — slots are acquired AFTER the existing `authz.Can` permission check and BEFORE any branch (fresh turn, HITL resume, approval re-emit) writes `Content-Type: text/event-stream`.

The default cap is `N=3` per user. That covers a realistic interactive workload:

- one stream reading replies on a phone,
- one stream driving the live chat in the browser, and
- one stream polling the tasks list.

A single abusive user can no longer exhaust process resources by opening dozens of parallel chats.

## 2. What triggers a 429

`POST /api/v1/chat/{conversation_id}` when the user already holds the maximum allowed slots returns:

```http
HTTP/1.1 429 Too Many Requests
Content-Type: application/json
Retry-After: 1

{"code":"sse_concurrency_exceeded","retry_after_s":1}
```

Crucially, the 429 is a plain JSON response — the api never writes the `text/event-stream` Content-Type. The frontend's existing `useConversationFlow.sendMessage` catches `response.ok === false` and renders the localized connection-error message; future polish (out of scope here) can switch on `body.code === "sse_concurrency_exceeded"` for a dedicated message.

## 3. Config knobs

| Env var | Default | Effect |
| --- | --- | --- |
| `SSE_MAX_PER_USER` | `3` | Hard cap on concurrent SSE streams per `user_id`. `0` disables the cap (every request bypasses the gate). Non-integer or negative values abort startup. |
| `LLM_RATELIMIT_ON_REDIS_DOWN` | `block` | Redis-down behavior. `block` rejects every request with HTTP 503; `local_fallback` drains an in-process token bucket. Operator-shared with the LLM rate limiter — one knob governs both gates. |
| `LLM_LOCAL_FALLBACK_REQUESTS_PER_HOUR` | unset | Sizes the local-fallback bucket. Required when `LLM_RATELIMIT_ON_REDIS_DOWN=local_fallback`; startup aborts if the policy is `local_fallback` and the budget is `<= 0`. |

The cap is per-process for the in-process gauge, but per-user across the whole cluster because Redis is the source of truth. With multiple api pods behind a load balancer, the counter is consistent.

## 4. Prometheus metrics

| Metric | Type | Labels | When it moves |
| --- | --- | --- | --- |
| `sse_concurrency_blocked_total` | Counter | `tier` | Increments once per rejection. No `user_id` label — cardinality and PII. |
| `sse_concurrency_inflight` | Gauge | none | Reflects the number of currently held slots in this process. |
| `ratelimit_redis_down_fallback_total` | Counter | `action` (`block` / `allow` / `deny`) | Shared with the LLM rate limiter. Increments whenever the Redis-down policy makes a decision. |

Useful PromQL:

```promql
# Rate of 429s per tier over the last 5 minutes.
sum by (tier) (rate(sse_concurrency_blocked_total[5m]))

# Process gauge — should be 0 when nobody is chatting.
sse_concurrency_inflight

# Distribution of Redis-down policy decisions during an outage.
sum by (action) (rate(ratelimit_redis_down_fallback_total[5m]))
```

## 5. What is NOT capped

- **Resume streams that reconnect within the 5-minute key TTL window** count as separate acquires. Each new HTTP request consumes resources, and the cap treats them honestly.
- **Internal callers** that bypass the chat proxy (the auto-titler service, draft-reply Router) — they hit Redis on their own throttles, not this counter.
- **Non-chat SSE endpoints** — none exist today. If one is added, it must opt in explicitly; the counter is not middleware.
- **Rejected requests** (`authz.Can` denied, business not found) — they short-circuit BEFORE the Acquire call, so they do not consume a slot.

## 6. How to inspect / disable

Inspect Redis state:

```bash
# Which users currently hold slots?
redis-cli KEYS 'sse:user:*'

# How many slots is a specific user holding?
redis-cli GET sse:user:{uuid}:active

# When does a leaked slot auto-expire?
redis-cli TTL sse:user:{uuid}:active
```

Each key has a 5-minute TTL so a crashed handler cannot leak slots forever — within minutes Redis cleans up.

Disable the cap entirely:

```env
SSE_MAX_PER_USER=0
```

Acquire short-circuits and returns a no-op release; the handler runs as if the cap never existed.

Smoke-test the cap (operator gate):

```bash
# Open three streams in background, then expect the fourth to fail fast.
for i in 1 2 3; do
  curl -N -H "Authorization: Bearer $JWT" -X POST \
    -d '{"text":"Hi"}' \
    "http://localhost:8080/api/v1/chat/${CONVO_UUID}" > /dev/null &
done
sleep 1
curl -i -H "Authorization: Bearer $JWT" -X POST \
  -d '{"text":"Hi"}' \
  "http://localhost:8080/api/v1/chat/${CONVO_UUID}"
# Expect: HTTP/1.1 429, body {"code":"sse_concurrency_exceeded","retry_after_s":1}
```

To verify Redis-down semantics:

```bash
docker compose stop redis
curl -i -H "Authorization: Bearer $JWT" -X POST \
  -d '{"text":"Hi"}' \
  "http://localhost:8080/api/v1/chat/${CONVO_UUID}"
# Expect: HTTP/1.1 503, body {"code":"rate_limit_unavailable"}
docker compose start redis
```

If `LLM_RATELIMIT_ON_REDIS_DOWN=local_fallback` is set with a positive `LLM_LOCAL_FALLBACK_REQUESTS_PER_HOUR`, the same step will admit traffic up to the bucket budget and then start emitting 503 once the bucket is drained.

## 7. Frontend behavior

The chat surface (`services/frontend/hooks/useConversationFlow.ts::sendMessage`) checks `response.ok` after the POST and falls through to the localized `tCommon('connectionError')` rendering when `false`. A follow-up frontend polish (deferred) is to inspect `await response.json()` and surface a dedicated message when `code === "sse_concurrency_exceeded"` — for now the generic error is acceptable because the 429 condition is a momentary user state, not a server failure.
