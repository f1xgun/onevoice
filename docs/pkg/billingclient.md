# `pkg/billingclient` — Billing Substrate HTTP Client

Typed HTTP client for the API's internal billing endpoints, consumed by
the orchestrator's `pkg/llm.Router` for write-path (`LogUsage`) and by
the cost-guard / daily-spend gate for read-path (`GetDailySpend`).

`client.go` keeps 1-line godoc + inline WHY comments; the contract,
operational characteristics, error envelope, and usage patterns live
here.

## Position in the stack

```
services/orchestrator/cmd/main.go
  → llm.NewRouter(llm.WithBilling(billingclient.New(apiBaseURL, nil)))
    → pkg/llm.Router.Chat
       → on success: go r.logBilling(ctx, ...) // fire-and-forget, 5s ctx
            → billingclient.Client.LogUsage
               → POST {baseURL}/internal/v1/billing/usage_logs
```

```
services/orchestrator/internal/costguard (cost-guard gate)
  → billingclient.Client.GetDailySpend
     → GET {baseURL}/internal/v1/billing/daily_spend?business_id=&date=
```

The orchestrator is the ONLY production caller. `services/api` mounts
the endpoints in `services/api/internal/handler/internal_billing.go`.

## Constructor shape — pinned to tokenclient's signature

```go
func New(baseURL string, httpClient *http.Client) *Client
```

MUST stay identical to `pkg/tokenclient.New` so the orchestrator wiring
site can treat the two internal-HTTP clients uniformly and a future
router-side retry policy can apply via `errors.Is(err, ErrTransient)`
across both.

`httpClient == nil` triggers `defaultHTTPClient()`, which honors the
`ONEVOICE_MTLS_*` env triplet through `pkg/mtls`. When mTLS is
enabled, the transport carries the orchestrator's leaf cert + CA root
so calls to `services/api`'s internal :8443 listener complete the
handshake. When disabled (unit tests against `httptest.NewServer`), the
transport stays plain — preserving back-compat with the test suite.

## mTLS failure mode — fall back, do not panic

`New` has no error return because changing its signature would break
the orchestrator's `cmd/main.go` wiring. So when mTLS is enabled but
the env is misconfigured (missing path, unreadable cert), the
constructor logs at `warn` level and falls back to a plain transport.

This is INTENTIONAL: an mTLS misconfiguration on the orchestrator boot
path is a deployment bug, but it is preferable to fail the next
`LogUsage` (which is fire-and-forget, silent-loss accounted in the
prometheus counter) than to panic the entire orchestrator binary.

A real mTLS environment surfaces the misconfiguration through the
warn log + the `llm_billing_post_failures_total{reason="transient"}`
counter spike on the first call.

## Timeouts — two-layer

| Layer                   | Value | Source                                   |
| ----------------------- | ----- | ---------------------------------------- |
| Caller context deadline | 5 s   | `pkg/llm/router.go:logBilling` ctx scope |
| Transport timeout       | 10 s  | `defaultHTTPTimeout` (this package)      |

The caller deadline is the real gate. The 10 s transport timeout is
the safety net for callers that pass `context.Background()` (no
deadline) — keeps a hung connection from leaking a goroutine forever.

## Wire contracts

### `POST /internal/v1/billing/usage_logs`

Body: JSON-encoded `*llm.UsageLog`.

| Response          | Sentinel returned          | Counter increment                            |
| ----------------- | -------------------------- | -------------------------------------------- |
| 200 / 204         | nil                        | —                                            |
| 400               | `ErrInvalidPayload`        | `llm_billing_post_failures_total{reason="invalid_payload"}` |
| 5xx               | `ErrTransient`             | `…{reason="transient"}`                      |
| other non-2xx     | bare error (no sentinel)   | `…{reason="unexpected_status"}`              |
| network failure   | `ErrTransient`             | `…{reason="transient"}`                      |

Pre-flight guards (no HTTP call):

| Trigger                | Sentinel              | Counter increment           |
| ---------------------- | --------------------- | --------------------------- |
| `log == nil`           | `ErrInvalidPayload`   | `…{reason="invalid_payload"}` |
| `log.BusinessID == uuid.Nil` | `ErrInvalidPayload` | `…{reason="invalid_payload"}` |
| `json.Marshal` failure | `ErrInvalidPayload`   | `…{reason="invalid_payload"}` |

The nil-BusinessID guard matches `services/api`'s repository contract
(`usage_logs.business_id` is `NOT NULL`) so system-level callers
(titler, review_drafter — both pass `uuid.Nil`) fail fast at the client
instead of generating a 400 round-trip per call.

### `GET /internal/v1/billing/daily_spend?business_id=&date=`

Returns the per-business cumulative LLM spend for the UTC calendar day
containing `day`.

| Response                            | Return value              | Sentinel              |
| ----------------------------------- | ------------------------- | --------------------- |
| 200 + well-formed body              | `(value, nil)`            | —                     |
| 200 + malformed body                | `(0, err)`                | `ErrInvalidPayload`   |
| 400                                 | `(0, err)`                | `ErrInvalidPayload`   |
| 5xx                                 | `(0, err)`                | `ErrTransient`        |
| other non-2xx                       | `(0, err)`                | (none)                |
| network failure                     | `(0, err)`                | `ErrTransient`        |

The day is always interpreted in UTC; callers in other time zones
still receive the UTC-day window the billing repository pins to. This
ensures the cost-guard gate, the API endpoint, and the underlying
Postgres query all share a single canonical day boundary.

`GetDailySpend` deliberately does NOT emit a Prometheus counter —
read-path failures fail closed at the cost-guard gate (a transient
read denies new spend until the read succeeds), which is itself
observable through the gate's own metrics.

## Error envelope

| Sentinel             | Meaning                                             | Caller treatment              |
| -------------------- | --------------------------------------------------- | ----------------------------- |
| `ErrInvalidPayload`  | Client / server-side determined the input is bad.   | Drop the log; do not retry.   |
| `ErrTransient`       | Network or server-side transient (5xx).             | Eligible for future retry policy via `errors.Is`. |
| (no sentinel)        | Unexpected status: likely proxy / auth misconfig.   | Surface to ops; do not retry. |

`errors.Is(err, ErrTransient)` and `errors.Is(err, ErrInvalidPayload)`
are the only API surface for classification. The bare-error case for
unexpected status (401/403/404/418/etc.) is deliberate so a reverse-
proxy misconfig fails CLOSED at the caller without retrying into the
misconfigured path.

## Idempotence + silent-loss policy

`LogUsage` calls are fire-and-forget from the orchestrator's view —
`pkg/llm/router.go` spawns `go r.logBilling(...)` after the user-visible
LLM response is already flushed. The error return is logged + counted
but NEVER blocks the user.

This means:

- A transient billing failure is a SILENT LOSS on that row.
- The accounting is recovered through the
  `llm_billing_post_failures_total{reason}` counter — operators
  reconcile to known-good rows via that counter, not via retry logic
  in this client.
- The endpoint is NOT idempotent at the wire level (no idempotence
  key); the substrate does the dedupe at the repository level.

If a future requirement demands at-least-once delivery, the right
place to add it is in `pkg/llm/router.go` (caller side) with an
idempotence key on `UsageLog` — not by adding retry logic inside this
client (which would break the silent-loss accounting and double-count
the prometheus counter).

See `pkg/billingclient/AGENTS.md` for the operator-facing version of
this policy.

## Constant discipline

| Constant                | Why it is a const                                                                 |
| ----------------------- | --------------------------------------------------------------------------------- |
| `usageLogsPath`         | Route declaration in `services/api` and the client call site can never drift.     |
| `dailySpendPath`        | Same discipline as `usageLogsPath` for the read path.                             |
| `defaultHTTPTimeout`    | Safety-net transport timeout for `context.Background()` callers.                  |
| `reasonTransient`, `reasonInvalidPayload`, `reasonUnexpectedStatus` | Metric label values: increment sites and test assertions cannot drift. Adding a new reason MUST update both the metric Help string in `pkg/metrics/llm.go` AND this doc. |

## Compile-time guarantees

```go
var _ llm.Writer = (*Client)(nil)
```

Asserts `*Client` satisfies `pkg/llm.Writer` (the narrow interface
`pkg/llm.Router` accepts via `WithBilling`). A build break here flags
drift if `pkg/llm.Writer` gains a method — and forces the change to
land in this client OR in the `llm.Writer` shape, not silently.

## Common usage patterns

### Production wiring (orchestrator)

```go
billingClient := billingclient.New(cfg.APIBaseURL, nil) // mTLS-aware default transport
router := llm.NewRouter(
    llm.WithBilling(billingClient),
    // ...
)
```

### Test wiring (unit tests)

```go
srv := httptest.NewServer(handler)
defer srv.Close()
client := billingclient.New(srv.URL, srv.Client())
err := client.LogUsage(ctx, &llm.UsageLog{BusinessID: bizID, ...})
```

`srv.Client()` honors the test server's TLS config when applicable;
passing it explicitly keeps the test free of env-driven mTLS state.

### Cost-guard gate

```go
spend, err := client.GetDailySpend(ctx, businessID, time.Now())
switch {
case errors.Is(err, billingclient.ErrTransient):
    // fail closed — deny new spend until the read recovers
case errors.Is(err, billingclient.ErrInvalidPayload):
    // alert ops; the gate is misconfigured
case err != nil:
    // unexpected status — surface to ops, fail closed
case spend >= cap:
    return ErrDailySpendExceeded
}
```

## Cross-references

- `pkg/billingclient/errors.go` — `ErrTransient`, `ErrInvalidPayload`
  sentinel declarations.
- `pkg/billingclient/AGENTS.md` — operator-facing silent-loss policy.
- `pkg/tokenclient` — constructor-shape twin; same mTLS / sentinel discipline.
- `pkg/llm.Writer` — narrow interface this client satisfies.
- `pkg/llm/router.go:logBilling` — production caller (fire-and-forget,
  5 s ctx deadline).
- `pkg/mtls` — mTLS env contract + path helpers.
- `services/api/internal/handler/internal_billing.go` — server side of
  both endpoints.
- `docs/llm-cost-guards.md` — daily-spend gate operator runbook.
