# pkg/billingclient — Internal API HTTP client for usage logs

Mirrors `pkg/tokenclient/` in shape. The orchestrator's `pkg/llm.Router`
uses this client (wired in `services/orchestrator/cmd/main.go` via
`llm.WithBilling(...)` — landing in plan 25a-05) to `POST` every
`*llm.UsageLog` row produced by a completed Chat() call to
`services/api`'s `POST /internal/v1/billing/usage_logs` endpoint
(mounted by plan 25a-04) over mTLS.

## Constructor

```go
billingclient.New(baseURL string, httpClient *http.Client) *Client
```

`httpClient == nil` builds a default 10s-timeout client whose transport
honors the `ONEVOICE_MTLS_*` env triplet via `pkg/mtls`. The signature
MUST stay byte-for-byte identical to `pkg/tokenclient.New` — Phase 25b's
hypothetical router-side retry policy applies uniformly to both clients
on the assumption.

## Sentinel errors

- `ErrTransient` — network failure (DNS / connection refused / TLS hiccup
  / canceled ctx) OR HTTP 5xx. Retrying may succeed.
- `ErrInvalidPayload` — local validation failure (nil log, missing
  BusinessID, marshal error) OR HTTP 400 from the API. Caller MUST NOT
  retry the same payload.

Errors chain via `fmt.Errorf("%w: ...", ErrTransient, ...)` so
`errors.Is(err, ErrTransient)` resolves through additional wrapping at
the call site.

Unexpected non-2xx, non-4xx, non-5xx responses (e.g. a stray 418) return
a bare error WITHOUT a sentinel — matches the tokenclient
"unexpected non-5xx" contract.

## Prometheus counter

```
llm_billing_post_failures_total{reason}
```

Labels: `transient | invalid_payload | unexpected_status`.

The counter is the audit signal for the silent-loss policy below. Any
new label value MUST update both the metric `Help` string in
`pkg/metrics/llm.go` and this AGENTS.md.

## Silent-loss policy — DO NOT add a queue/outbox

The router's `logBilling` goroutine drops the error returned by
`LogUsage` on the floor so the user-visible LLM response is never
blocked by a billing failure. This is a deliberate v1.4 free-beta
trade-off:

| Failure mode | Outcome | Operator signal |
|---|---|---|
| API down for 30s | Up to 30s × QPS rows lost | Counter spikes; Grafana alert |
| Malformed payload | One row lost per call site | Counter spikes; root-cause via log |
| mTLS handshake fail | All rows lost until cert fixed | Counter spikes hard; ops page |

**Forbidden patterns** (REJECTED in code review):

- ❌ Adding an in-memory queue / channel buffer to "retry later" — the
  process is the unit of failure; a queue in the same process just
  changes when the rows are lost.
- ❌ Adding a disk outbox under `~/.onevoice/billing/` — v1.5+ may add
  one, but it requires a separate plan with crash-safety review.
- ❌ Adding `httpClient.Timeout` shorter than 10s — the per-call deadline
  comes from the caller's context (`5s` set by `pkg/llm/router.go:logBilling`
  in plan 25a-02).
- ❌ Caching responses — `LogUsage` is a write-only POST.
- ❌ Per-client retries — symmetry with `pkg/tokenclient` matters more
  than micro-resilience; Phase 25b decides router-wide retry policy.

## Compile-time guard

```go
var _ llm.Writer = (*Client)(nil)
```

If `pkg/llm.Writer` gains a method, the build break here is the earliest
signal to update billingclient in lockstep with the orchestrator wiring.

## Files

- `client.go` — `*Client` + `LogUsage` + `defaultHTTPClient` (mTLS-aware).
- `errors.go` — `ErrTransient` + `ErrInvalidPayload` sentinels.
- `client_test.go` — 11 httptest-driven tests (2xx / 4xx / 5xx / network
  / ctx-canceled / nil-input / mTLS-enabled handshake).

## Related plans

- **25a-01** — mTLS substrate (`pkg/mtls`) that the default client wires.
- **25a-02** — `pkg/llm.UsageLog` shape + JSON tags that this client serializes.
- **25a-04** — `POST /internal/v1/billing/usage_logs` API handler that consumes the body.
- **25a-05** — orchestrator-side wiring via `llm.WithBilling(billingclient.New(cfg.APIInternalURL, nil))`.
- **25b** — hypothetical router-wide retry policy reading `errors.Is(err, ErrTransient)`.
