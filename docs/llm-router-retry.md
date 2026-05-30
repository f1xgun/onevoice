# LLM Router Retry Policy

The LLM Router fronts every chat-turn LLM call. When the registry has more than
one provider configured for the requested model, the router will retry a failed
first attempt once against a sibling registry entry — a different provider
serving the same model. This document is the operator runbook for that policy.

## What the retry does

A chat call walks at most two candidates:

1. The primary candidate is whatever the selector returns first under the
   requested strategy (`StrategyCost` or `StrategySpeed`).
2. If the primary attempt fails AND the error is classified as transient AND
   the registry contains a sibling entry for the model, the router dials the
   sibling once.

The retry is policy-only: the LLM POST is naturally idempotent at the API
layer (no side effects, the prompt is the entire input), so replaying the
request against a different provider is safe. No backoff is applied; the
retry fires immediately after the primary fails.

Billing fires exactly once and only for the successful attempt. If the
primary fails and the sibling succeeds, only the sibling's UsageLog is
written. The failed primary attempt is never billed even if the provider
surfaced a partial Usage object on the error path.

## What counts as transient

Transient errors trigger the retry:

| Source                  | Condition                                              |
| ----------------------- | ------------------------------------------------------ |
| `net.Error`             | `Timeout()` or `Temporary()` returns true              |
| OpenAI SDK `APIError`   | `HTTPStatusCode` is 429 or in the 500–599 range        |
| Anthropic SDK `Error`   | `StatusCode` is 429 or in the 500–599 range            |
| Untyped error string    | Contains `429`, `502`, `503`, or `504`                 |

Non-transient errors are returned immediately without a sibling attempt.
These include any 4xx other than 429 (bad request, invalid model, missing
auth), malformed responses, and any error whose message does not match the
substring fallback. A non-transient error means "the caller is at fault and
a sibling can't help."

## How to register a sibling entry

Configure two providers that both serve the same model. The most common pair
is `openrouter` + `anthropic` for Anthropic Claude models, or `openrouter` +
`openai` for OpenAI models:

```bash
# .env (orchestrator)
OPENROUTER_API_KEY=<valid>
ANTHROPIC_API_KEY=<valid>
LLM_MODEL=anthropic/claude-sonnet-4-6
```

The model registry will record two entries for `anthropic/claude-sonnet-4-6`
— one for each provider. The selector's strategy ordering picks the primary;
the other becomes the retry target. Pricing for the registered providers is
maintained in [`docs/llm-pricing.md`](llm-pricing.md).

If only one provider is configured for a model, the retry policy is a no-op:
there is no sibling to retry against, and the original error is returned.

## Prometheus counters to watch

The router emits `llm_router_retry_total` after every chat-turn attempt:

```
llm_router_retry_total{result="<result>", attempt="<attempt>"}
```

| Label value (`result`) | Meaning                                                               |
| ---------------------- | --------------------------------------------------------------------- |
| `success`              | Provider returned 200 OK.                                             |
| `retrying`             | Primary failed transient; sibling about to be attempted.              |
| `exhausted`            | Transient failure with no remaining sibling (or sibling also failed). |
| `nonretryable`         | Non-transient error; no retry was attempted.                          |

| Label value (`attempt`) | Meaning                              |
| ----------------------- | ------------------------------------ |
| `first`                 | Primary candidate (registry index 0). |
| `second`                | Sibling candidate (registry index 1). |

Sample PromQL queries:

```
# Success rate of the retry path: how often does the sibling save the call?
sum(rate(llm_router_retry_total{result="success",attempt="second"}[5m]))
  /
sum(rate(llm_router_retry_total{result="retrying"}[5m]))

# Total provider outages absorbed by the retry policy:
sum(increase(llm_router_retry_total{result="success",attempt="second"}[1h]))

# Customer-visible failures (transient that nobody could serve):
sum(rate(llm_router_retry_total{result="exhausted"}[5m]))
```

A rising `exhausted` rate with the corresponding `retrying` rate is the
signal for a multi-provider outage — both candidates are degraded at the same
time. A rising `nonretryable` rate is the signal for a client-side regression
(bad prompts, misconfigured tools, deprecated model IDs).

## What is NOT retried

- **`ChatStream`** is not retried. Mid-stream errors are not safely
  idempotent — the channel may have already emitted partial bytes to the
  frontend, and replaying the request against a sibling would surface
  duplicate content. Frontends that hit a streaming error should re-issue
  the prompt on a fresh connection.
- **Non-transient errors** (4xx other than 429, validation failures,
  upstream config errors) return immediately — the caller is at fault and
  a sibling can't help.
- **Single-candidate registries** return the original error without a
  retry attempt because there is no sibling to attempt against. The
  `exhausted/first` counter increments in this case so the operator can
  see the un-protected surface area.

## Disabling the retry

There is no operator-facing knob to disable the retry in this release.
Removing the sibling registry entry (unset one of the API keys) is the
documented way to opt out: the router will then fall back to the
single-candidate path and the policy becomes a no-op for that model.

If a future regression makes the retry harmful, file a follow-up to add a
boolean `LLM_ROUTER_RETRY_ENABLED` env knob; the policy is a single guarded
branch in `pkg/llm/router.go` so the addition is mechanical.
