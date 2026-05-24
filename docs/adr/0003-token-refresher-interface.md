# ADR-0003 — Keep `service.TokenRefresher` interface despite single production implementation

- **Status:** Accepted
- **Date:** 2026-05-24
- **Scope:** `services/api/internal/service/integration.go`, `services/api/internal/wire/google_refresher.go`

## Context

`services/api/internal/service.TokenRefresher` is a one-method
interface used by `integrationService.GetDecryptedToken` to refresh
expired OAuth tokens transparently before returning them to callers:

```go
type TokenRefresher interface {
    RefreshToken(ctx context.Context, refreshToken string) (
        accessToken string,
        newRefreshToken string,
        expiresIn int64,
        err error,
    )
}
```

Current state:

- **Production implementations:** exactly **one** —
  `googleTokenRefresher` in
  `services/api/internal/wire/google_refresher.go` (HTTP POST to
  `oauth2.googleapis.com/token`, ~80 LOC).
- **Test implementations:** one — `mockTokenRefresher` in
  `services/api/internal/service/integration_test.go`, used by 5
  distinct test scenarios (happy refresh, refresh-token rotation,
  refresh failure, expired-with-no-refresher, etc.).
- **Other MVP platforms (Telegram, VK, Yandex.Business)** do not need
  OAuth refresh: Telegram bot tokens never expire, VK uses long-lived
  user tokens (expired = user re-paste), Yandex.Business is
  cookie-based RPA. So Google is the only refresh-capable platform
  today, and the next-most-likely candidate (`/improve-codebase-architecture`
  v3 — 2026-05-24) classified `TokenRefresher` as a
  **Speculative** "single-impl seam" deepening opportunity, i.e. a
  candidate to collapse into the concrete `*googleTokenRefresher`.

## Decision

**Keep `service.TokenRefresher` as an interface.** Do not collapse it
to a concrete type.

The interface earns its keep on two independent grounds, either of
which alone would justify it; together they make collapse a clear
regression.

1. **Layering / decoupling.** `service/integration.go` must stay free
   of HTTP/OAuth2 plumbing (`net/http`, `encoding/json`, `net/url`,
   string-formatted error parsing). Today the impl lives in `wire/`
   alongside the rest of the per-process wiring; the service depends
   only on the abstraction. Collapsing the interface would either
   force the impl back into `service/` (polluting the layer with
   transport concerns) or force `service/` to import `wire/` (which
   inverts the dependency direction the package layout enforces).

2. **Testability.** The 5 refresh tests in `integration_test.go` need
   to inject controlled refresh behavior — failure on first call,
   rotation of the refresh token, success returning specific
   `expires_in`, etc. The current shape lets each test pass a
   `mockTokenRefresher{refreshFunc: ...}` parameterised inline.
   Without the interface, tests would have to stub an `httptest`
   server, build query-string parsing assertions against form data,
   and re-encode the test as an integration test of the Google token
   endpoint contract — a much larger surface for what each test
   actually wants to assert (the service-side orchestration).

## Alternatives considered

### A. Collapse to concrete `*googleTokenRefresher` in `wire/`

```go
type integrationService struct {
    refresher *wire.googleTokenRefresher // unexported in wire — broken
}
```

Rejected — `service/` cannot import `wire/` without an import cycle,
and even if it could, the field type would expose `wire`'s private
naming to the service layer.

### B. Collapse and move `googleTokenRefresher` into `service/`

```go
// services/api/internal/service/google_token_refresher.go
type googleTokenRefresher struct {
    clientID, clientSecret string
    httpClient *http.Client
}
```

Rejected — `service/` picks up `net/http`, `encoding/json`, `net/url`,
`io`, and the Google-specific error response struct. The layer that
today is pure business logic becomes Google-aware. Adding any second
platform (hypothetical Telegram personal-OAuth, MS Graph, Slack)
would require either a `switch platform string` inside `service/` or
re-introduction of an interface — i.e. the seam returns the moment a
second adapter exists.

### C. Replace interface with a function value

```go
type TokenRefreshFunc func(ctx context.Context, refreshToken string) (
    string, string, int64, error,
)
```

Rejected — equivalent leverage at the call site, but loses two
properties: (a) the Go interface gives an obvious documentation
anchor and a name (`TokenRefresher`) that the audit log, error
messages, and refactoring tools all latch onto; (b) test
implementations can carry state (e.g. `callCount` for assertion of
non-redundant refresh) without closure capture gymnastics. The
function-value form would force tests to wrap state in a closure
struct anyway, so there is no shape simplification — just a name lost.

### D. SKIP without ADR

Rejected — `/improve-codebase-architecture` v3 surfaced this
candidate exactly because the heuristic "one prod impl looks like a
hypothetical seam" fires here. Without this record, the same
Speculative candidate would reappear on every future sweep. ADRs
exist to record load-bearing skip decisions so reviewers (human and
AI) can see the prior reasoning instead of re-deriving it. ADR-0002
already mentions `TokenRefresher` as an example of an earned seam; this
ADR is the dedicated record.

## Consequences

**Adding a new refresh-capable platform** should add a new
implementation of `service.TokenRefresher` in `wire/` (e.g.
`wire/microsoft_refresher.go` implementing
`service.TokenRefresher`) and select between them at wire time based
on the integration's `platform` field. The current single-`refresher`
field on `integrationService` would graduate to a
`map[string]TokenRefresher` keyed by platform — a 5-LOC change to the
field type and the dispatch site. No interface redesign required.

**Test mocks remain the supported test path.** The
`mockTokenRefresher` in `integration_test.go` is the canonical way to
exercise `GetDecryptedToken`'s refresh orchestration without an HTTP
round-trip; new refresh-related tests should follow the same shape.

## Reconsider when

This ADR should be revisited and likely superseded by an ADR-0004 if:

- **The single production impl shape stops fitting the interface.**
  E.g. if a future provider requires a different refresh credential
  (mTLS, PKCE, client assertion) or returns additional metadata
  (scopes, id_token), the right move is to widen `RefreshToken`'s
  signature or split the interface — not to collapse it.
- **The refresh orchestration inside `GetDecryptedToken` itself gets
  extracted into a `tokenRefreshCoordinator`** (a separate deepening
  opportunity surfaced alongside this audit). At that point the
  ownership of the `TokenRefresher` field may move from
  `integrationService` to the coordinator, but the interface itself
  remains — only its consumer changes.

At any of those triggers, this ADR should be revisited and superseded
with the new context.

## Related

- [ADR-0001](0001-prompt-locale-pair-rendering.md) — methodology
  precedent (SKIP + ADR for a single-impl seam).
- [ADR-0002](0002-persistence-domain-side-effects-split.md) —
  references `TokenRefresher` as an example of a justified seam in
  the service layer; this ADR is the dedicated record.
