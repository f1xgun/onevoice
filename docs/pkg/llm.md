# pkg/llm — Selector + Router

The orchestrator's LLM front door is split across two collaborating seams:

- **Selector** (`pkg/llm/selector.go`) picks a provider for a model, tracks
  rolling latency + health, and emits per-call Prometheus metrics.
- **Router** (`pkg/llm/router.go`) handles the cross-cutting concerns that
  bracket every call — rate limit gate, billing write, retry policy — and
  delegates provider choice to the Selector.

This document is the design contract for that pair. Code in `selector.go` and
`router.go` keeps only 1-line godoc and inline WHY comments; everything else
about *why* the policies are the shape they are lives here.

## The Selector / Router split

Routing concerns split cleanly along two axes:

| Concern                           | Owner    |
| --------------------------------- | -------- |
| "Which provider for this model?"  | Selector |
| Rolling latency / health state    | Selector |
| Per-call Prometheus emission      | Selector |
| Rate limit gate                   | Router   |
| Retry policy across candidates    | Router   |
| Billing write (cache-aware)       | Router   |
| `llm_router_retry_total` counter  | Router   |

Selector exposes three methods (`Pick`, `Candidates`, `Record`). All three are
safe for concurrent use — the Router fans them out across goroutines. The
default implementation is `defaultSelector`; `NewSelector` exists so trace /
fault-injection decorators can wrap the production policy instead of
re-implementing the rolling window.

The `Candidate` struct pairs an entry with its registered Provider so that
Router.Chat can walk the priority-ordered list without re-resolving providers
from `entry.Provider` strings on every step.

## Candidate ordering

`buildCandidates` is the single ordering pass driven by both `Pick` (returns
the head) and `Candidates` (returns the whole list). Two phases:

1. **Filter:** drop disabled entries and entries whose registered Provider
   was never wired via `WithProvider`. An entry with no resolvable Provider
   is invisible to the Router — the registry config might mention it, but
   no goroutine can dial it.
2. **Stable sort:** healthy entries first, then strategy tie-break inside
   each health bucket. Registry order is preserved for equal keys so the
   ordering is deterministic across calls.

Unhealthy and degraded entries appear AFTER healthy ones but are NOT
filtered out. The retry path can attempt a known-bad sibling if the primary
fails, which is preferable to surfacing the failure to the caller when the
sibling might have recovered between health checks.

### Strategy tie-breaks

- **`StrategyCost`:** compares `avgCost = (Input+Output)/2` per 1M tokens.
  A model with cheap input but expensive output is not blindly preferred
  over a balanced model — we score on the symmetric midpoint.
- **`StrategySpeed`:** compares rolling `AvgLatencyMs`. Zero (no
  measurements yet) ranks LAST so a fresh entry doesn't win on a phantom
  "zero is smallest" comparison. Once a fresh entry has even one sample
  it competes fairly.

## Pick fallback semantics

`Pick` returns the first candidate (which `buildCandidates` already sorted
healthy-first). If every healthy provider is unavailable, `Pick` still
returns the highest-priority candidate regardless of health — this prevents
a single-provider outage from permanently deadlocking the Router once the
provider recovers. `ErrNoProvider` only fires when zero enabled+registered
candidates exist.

## Rolling metrics + health transitions

`Record` is the policy entry point: every call outcome folds into the
rolling window and may transition the entry's `HealthStatus`.

Tunables (package-scope `const`s):

| Knob                       | Value | Why                                           |
| -------------------------- | ----- | --------------------------------------------- |
| `latencyWindow`            | 100   | Samples per (provider, model) for AvgLatencyMs |
| `healthDegradedRate`       | 0.2   | Failure rate above which → Degraded            |
| `healthDownRate`           | 0.5   | Failure rate above which → Down                |
| `healthRecoverySuccessMin` | 3     | Consecutive successes to flip back to Healthy  |

Mirroring: `Record` writes `HealthStatus` + `AvgLatencyMs` + `LastCheckedAt`
back onto the `ModelProviderEntry` pointer so the next `Pick` (and any
registry consumer that reads entries for an admin / status endpoint) sees
the new policy state without consulting the Selector.

### Per-call Prometheus emission

`Record` emits `metrics.RecordLLMRequest(model, provider, status, wall)` at
the end of every call. Two important subtleties:

- The emission is GATED on `outcome.Model != ""`. Direct `Record` callers
  (integration tests etc.) may leave Model empty; in that case the
  Selector skips the histogram write so test data does not pollute the
  prometheus series with empty labels. The `Invoke` helper always fills
  `Model` so production paths always emit.
- Two latency fields are tracked for two purposes:
  - `Latency` — provider-reported end-of-response duration. Drives the
    rolling-window `AvgLatencyMs` used by `StrategySpeed`. Zero for
    streaming starts (channel-open instant is not user-perceived), so
    streaming starts do not pollute the rolling window.
  - `Wall` — wall-clock duration as seen by the caller (`Invoke` fills
    this from `time.Since(start)`). Drives the prometheus per-call
    latency histogram. Always non-zero for production paths.

## LRU eviction policy

The `metrics` map is keyed by `provider + ":" + model`. With many model
variants and routing strategies the unique key space is unbounded over time,
so the map has a hard cap with single-pass LRU eviction.

`metricsMaxEntries = 1000` (var, not const, so tests can lower it without
inserting 1000 fixtures). On insert into a full map, `evictOldestMetricLocked`
runs a linear scan to drop the single least-recently-touched entry.

A linear scan is fine because:

1. The map is bounded at 1000 entries, so scan cost is sub-millisecond.
2. Eviction only runs on the cold path of inserting into a full map.
3. There is no background goroutine to lifecycle.

The alternative (heap-backed LRU, doubly-linked list) would add the same
sub-millisecond cost but require a goroutine-safe LRU type — not worth it
at this size.

## The Invoke helper

`Invoke[T]` brackets a single Selector cycle — pick a provider, run the
caller's closure, record the outcome, return. Two reasons it exists:

1. Callers don't have to remember to call `Record` after every Chat /
   ChatStream — `Invoke` does it unconditionally.
2. The blocking and streaming paths share the bookkeeping verbatim, so
   they cannot drift across two near-identical Chat / ChatStream blocks.

The closure shape returns `(result, providerLatency, error)`. Zero
`providerLatency` means "don't sample me" (used by ChatStream so the
channel-open instant doesn't pollute the rolling window).

On `Pick` failure the closure is NOT called and `Record` is NOT invoked —
the error has no provider entry to attribute. The Selector seam owns retry
policy, not `Invoke`; if multi-provider retry ever lands at `Invoke` it
goes on the Selector interface, not duplicated across each `Invoke` call
site.

Note: `Router.Chat` does NOT use `Invoke` because it owns the two-attempt
retry walk; it calls `Selector.Candidates` + `Selector.Record` directly.
`Router.ChatStream` does use `Invoke` because streaming is single-attempt.

## Router options

Functional-option constructor. Production wires:

- `WithProvider(p)` — register a Provider implementation by name.
- `WithRateLimiter(rl)` — gate per-user/tier daily spend. Without it the
  gate is a no-op.
- `WithBilling(w)` — accept the narrow `Writer` interface so production
  can pass a `Writer`-only HTTP adapter (`pkg/billingclient`) without the
  orchestrator depending on read-path methods. Any `BillingRepository`
  also satisfies `Writer` via interface embedding, so existing
  `WithBilling(BillingRepository)` callers continue to compile unchanged.
- `WithCommission(cfg)` — commission policy applied per call.

Test-only:

- `WithSelector(s)` — inject a fake Selector that answers `Pick` /
  `Candidates` / `Record` without the registry+entries dance. Production
  callers don't need this; `NewRouter` auto-wraps a `defaultSelector` from
  the registry and the providers registered via `WithProvider`.
- `WithRateLimitChecker(rlc)` — inject any `RateLimitChecker` (the same
  seam as `WithRateLimiter` but accepting the interface so tests can pass
  fakes).

## Rate limit gate

`checkRateLimit` is shared by `Chat` and `ChatStream` so they cannot drift.
Two short-circuits:

1. No rate limiter wired → skip.
2. `req.UserID == uuid.Nil` (cluster-internal calls like titler /
   review_drafter) → skip.

When the limiter returns an error, it is passed through verbatim so callers
can branch on the sentinels (`ErrDailySpendExceeded`,
`ErrRateLimitUnavailable`) directly. When `allowed == false`,
`ErrRateLimitExceeded` is returned.

The `tier` defaults to `"free"` when `req.Tier == ""`.

## Retry policy — Chat (blocking)

`Chat` walks at most two candidates (the primary + one sibling) on
transient errors. The full operator-facing description of the retry policy
— what counts as transient, the `llm_router_retry_total` counter labels,
sample PromQL — lives in [`docs/llm-router-retry.md`](../llm-router-retry.md).
This section covers the code-shape WHYs.

Single attempt cap (`maxChatAttempts = 2`): primary pick + one sibling.
Same-entry retry is intentionally skipped — without backoff it amplifies
provider outages, and the second-most-preferred registry entry is a
stronger fallback signal than a same-shot retry.

The `retryLabel(attempt)` helper maps attempt 0 → "first", 1 → "second",
and >=2 → "unknown". The label vocabulary is fixed at {first, second}; any
future policy change to N>2 attempts must update both the helper and the
counter's documented label set — `"unknown"` ensures the change is
SCREAMING-loud in dashboards instead of silently minting new series.

Per-attempt flow:

1. Call `Provider.Chat`. Time it.
2. Feed `Selector.Record` on every attempt — both successful retries AND
   exhausted retries inform the next Pick.
3. Success path:
   - Emit `llm_router_retry_total{result="success", attempt=…}`.
   - Stamp `resp.Provider` from the entry (the registry knows the canonical
     provider name; the provider implementation does not).
   - Fire-and-forget billing write IF `r.billing != nil` AND
     `req.BusinessID != uuid.Nil`. The `usage_logs.business_id` column is
     `NOT NULL` and the repository rejects nil-BusinessID rows, so
     system-level callers (titler, review_drafter) that pass `uuid.Nil`
     are silently skipped.
4. Failure path:
   - Non-transient → emit `nonretryable` counter, return.
   - Transient + sibling available (attempt 0 and len ≥ 2) → emit
     `retrying`, continue loop.
   - Transient + no sibling → emit `exhausted`, return.

Billing fires exactly once and only for the successful attempt's response
— the failed first attempt is never billed even if its partial response
surfaced Usage tokens.

## Retry policy — ChatStream (streaming)

`ChatStream` is intentionally NOT retried. Mid-stream errors are not safely
idempotent: the channel may have already emitted partial chunks to the
caller, and replaying the request against a sibling would surface duplicate
content.

ChatStream does NOT bill; the terminal turn (non-streaming) accounts the
cost.

Callers that need fault tolerance on the streaming path should fall back to
non-streaming `Chat` (the retried path) or re-issue the prompt on a fresh
connection.

## Cache-aware billing

`logBilling` runs on its own goroutine with a 5-second deadline
(`billingPostTimeout`) so a hung downstream billing endpoint cannot
accumulate goroutines forever.

The provider cost formula is cache-aware (Anthropic prompt-cache model):

```
billable_input = InputTokens*1.0
               + CacheReadTokens*0.1
               + CacheCreationTokens*1.25
provider_cost  = billable_input  * InputCostPer1MTok  / 1_000_000
               + OutputTokens    * OutputCostPer1MTok / 1_000_000
```

Multipliers:

- `cachePricingMultiplierRead = 0.1` — cache hit billed at 0.1× input rate.
- `cachePricingMultiplierCreation = 1.25` — cache write billed at 1.25×
  input rate (5-minute ephemeral cache tier).

Providers that do not surface cache breakdowns (OpenAI, OpenRouter,
SelfHosted) leave `CacheReadTokens` / `CacheCreationTokens` at zero, so
the formula collapses to `InputTokens * InputCostPer1MTok / 1_000_000` —
the pre-Phase-25a behavior.

`tokensPerMillion = 1_000_000` is the divisor for converting per-1M-token
list prices (the unit billing providers publish) into a per-token cost.

The commission policy (`CalculateCommission(providerCost, mode, tier)`) is
applied at the same point so a single billing write captures the user's
final cost in one row.

## Sentinel errors

- `ErrNoProvider` — no enabled+registered provider serves the model.
- `ErrRateLimitExceeded` — the rate limit gate rejected the request.

Provider-specific transient/non-transient classification (`isTransientLLMError`)
lives in a sibling file and is documented in
[`docs/llm-router-retry.md`](../llm-router-retry.md).

## Pointers to related docs

- [`docs/llm-router-retry.md`](../llm-router-retry.md) — operator runbook
  for the retry policy, transient-error classification, PromQL examples.
- [`docs/llm-pricing.md`](../llm-pricing.md) — list pricing per provider.
- [`docs/llm-cost-guards.md`](../llm-cost-guards.md) — per-business daily
  spend gate.
