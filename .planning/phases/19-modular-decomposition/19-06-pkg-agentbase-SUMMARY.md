---
plan: 19-06
phase: 19
slug: pkg-agentbase
subsystem: pkg/agentbase
tags: [refactor, agents, dedupe, hitl, interfaces]
wave: 1
status: complete
requirements: [SC-04]
dependency_graph:
  requires: []
  provides:
    - pkg/agentbase.TokenResolver  # consumed by plan 19-07 in 4 agents
    - pkg/agentbase.Dispatcher
    - pkg/agentbase.ErrorClassifier
    - pkg/agentbase.FuncClassifier
    - pkg/agentbase.TokenInfo
  affects:
    - services/agent-telegram      # plan 19-07 will consume
    - services/agent-vk            # plan 19-07 will consume
    - services/agent-yandex-business  # plan 19-07 will consume
    - services/agent-google-business  # plan 19-07 will consume
tech_stack:
  added: []
  patterns:
    - interface-first design with default impls behind New*() constructors
    - compile-time interface assertion (var _ Iface = (*impl)(nil))
    - panic on nil required deps; accept nil for optional deps
    - function-as-interface adapter (FuncClassifier)
    - verbatim-copy extraction (no logic edits during refactor)
key_files:
  created:
    - pkg/agentbase/token_resolver.go
    - pkg/agentbase/dispatcher.go
    - pkg/agentbase/error_classifier.go
    - pkg/agentbase/token_resolver_test.go
    - pkg/agentbase/dispatcher_test.go
  modified: []
  deleted: []
decisions:
  - id: D-AB-01
    summary: TokenInfo carries UserToken on the canonical struct (not per-platform variants)
    rationale: VK is the only consumer, but the struct stays uniform — non-VK platforms get an empty string. Avoids 4× per-agent TokenInfo definitions and matches what tokenclient.TokenResponse already exposes.
  - id: D-AB-02
    summary: dedupeGate / dedupeStore bodies lifted byte-for-byte from agent-telegram handler.go
    rationale: This is a structural refactor. Same slog message strings ("hitl dedupe claim failed; proceeding without dedupe", "hitl dedupe cached result malformed; returning generic duplicate", "hitl dedupe store failed; result returned but not cached"), same ClaimOutcome switch, same TaskID rewrite — so plan 19-07 produces a zero-diff agent migration when grep'ing slog/error messages.
  - id: D-AB-03
    summary: dedupeStore receives *a2a.ToolResponse directly (not a JSON string)
    rationale: hitldedupe.DedupeClient.Store accepts interface{} and json-marshals internally. The plan's pseudocode showed pre-encoding via json.Marshal but the production handler.go does the direct call — fidelity wins to keep plan 19-07 a true 1:1 swap.
  - id: D-AB-04
    summary: No "default" ErrorClassifier with shared keyword list
    rationale: Phase 19-RESEARCH §5c documents that the four platforms' permanent error markers are too different (HTTP string match for Telegram, VKError code match for VK, ErrSessionExpired sentinel for Yandex, Google API status strings for Google). FuncClassifier adapts each platform's existing one-line classify function instead.
  - id: D-AB-05
    summary: One file per interface (3 source files), not a single agentbase.go
    rationale: Each interface owns ~50-140 LOC including comments — splitting matches pkg/hitldedupe/dedupe.go scale and keeps each file readable in isolation. Also makes diffs in plan 19-07 land cleanly per interface.
metrics:
  duration_minutes: 22
  completed_at: 2026-05-09
  tasks_completed: 1
  loc_added_source: 260
  loc_added_tests: 504
  loc_total: 768
  files_created: 5
  test_count: 14
---

# Phase 19 Plan 19-06: pkg/agentbase — Shared Agent Building Blocks

**One-liner:** Create new `pkg/agentbase/` package exposing `TokenResolver`, `Dispatcher`, `ErrorClassifier` interfaces with default implementations extracted from the 4× duplications in services/agent-* — no consumers wired yet (plan 19-07's job).

## What was built

A new top-level Go package `pkg/agentbase` (sibling to `pkg/tokenclient`, `pkg/a2a`, `pkg/llm`, `pkg/hitldedupe`) with three single-responsibility files plus their tests.

### `pkg/agentbase/token_resolver.go` (78 LOC)

```go
type TokenInfo struct {
    AccessToken string
    UserToken   string
    ExternalID  string
}

type TokenResolver interface {
    GetToken(ctx context.Context, businessID, platform, externalID string) (TokenInfo, error)
}

func NewTokenResolver(c *tokenclient.Client) TokenResolver
```

Default impl `tokenResolverImpl` wraps `*tokenclient.Client.GetToken` and remaps `tokenclient.TokenResponse` → `TokenInfo` (3 fields: AccessToken, UserToken, ExternalID). Other fields on TokenResponse — Metadata, ExpiresAt, UserTokenExpires, IntegrationID — are intentionally NOT exposed because no agent currently consumes them (R5 enforcement). `NewTokenResolver` panics if client is nil (boot-time fail-fast).

### `pkg/agentbase/dispatcher.go` (137 LOC)

```go
type Dispatcher interface {
    Dispatch(
        ctx context.Context,
        req a2a.ToolRequest,
        exec func(context.Context, a2a.ToolRequest) (*a2a.ToolResponse, error),
    ) (*a2a.ToolResponse, error)
}

func NewDispatcher(dedupe *hitldedupe.DedupeClient, classifier ErrorClassifier) Dispatcher
```

Default impl `dispatcherImpl` runs the canonical four-step sequence:
1. **dedupeGate** — return early on `ClaimOutcomeInFlight` (response: `Error: "duplicate: already in flight"`) or `ClaimOutcomeDuplicate` (response: cached JSON unmarshalled, with `TaskID` rewritten to the replay's TaskID). Empty `ApprovalID` or nil dedupe client bypasses the gate entirely.
2. **exec** — caller-supplied per-tool work (the platform switch on `req.Tool` stays in each agent).
3. **classifier.Classify** — wraps permanent errors as `*a2a.NonRetryableError`. Nil classifier is a no-op.
4. **dedupeStore** — best-effort cache of successful responses (`err == nil && resp != nil`). Errors logged via `slog.WarnContext`, not propagated.

`dedupeGate` and `dedupeStore` bodies are byte-identical to `services/agent-telegram/internal/agent/handler.go:88-130` (same slog messages, same outcome mapping) — plan 19-07 will produce a zero-behavior-diff swap.

### `pkg/agentbase/error_classifier.go` (45 LOC)

```go
type ErrorClassifier interface {
    Classify(err error) error
}

type FuncClassifier func(error) error
func (f FuncClassifier) Classify(err error) error  // nil-safe identity
```

`FuncClassifier` adapts the existing per-agent `classifyTelegramError` / `classifyVKError` / `classifyYandexError` / `classifyGBPError` functions into the interface without touching their bodies. No default impl is provided — per RESEARCH §5c, a shared keyword list would be wrong for any of the four platforms.

### Compile-time invariants (per D-05)

```go
var _ TokenResolver = (*tokenResolverImpl)(nil)
var _ Dispatcher    = (*dispatcherImpl)(nil)
```

### Test coverage

- **`token_resolver_test.go`** — 5 tests: nil-client panics, full delegation (asserts `business_id`/`platform`/`external_id` propagation + `AccessToken`/`UserToken`/`ExternalID` round-trip), empty-externalID propagation (the API fallback contract), no-UserToken stays empty (telegram/yandex/google case), error propagation (404 → empty TokenInfo + wrapped err).
- **`dispatcher_test.go`** — 11 tests against a real `*hitldedupe.DedupeClient` backed by `miniredis` (same harness as `pkg/hitldedupe/dedupe_test.go`):
  - constructor accepts both nils
  - exec called exactly once on happy path
  - classifier wraps non-retryable errors and exposes them via `errors.As(err, &*NonRetryableError)`
  - nil classifier passes errors through untouched
  - nil FuncClassifier acts as identity (defensive path inside FuncClassifier itself)
  - empty ApprovalID bypasses the gate AND does not touch Redis
  - in-flight dedupe short-circuits exec, returns canonical `"duplicate: already in flight"` with rewritten TaskID
  - duplicate dedupe returns cached response with rewritten TaskID and original Result fields intact
  - malformed cached JSON falls back to `"duplicate: cached result unavailable"` without invoking exec
  - successful exec persists to Redis; second dispatch sees ClaimOutcomeDuplicate (round-trip proof)
  - failed exec leaves the `executing` sentinel intact (replay must be free to retry)
  - explicit ordering contract: gate → exec → classifier → store, with sentinel inspection to prove store skipped after classified non-nil error.

## Verification

| Gate | Command | Result |
|------|---------|--------|
| Package compiles | `cd pkg && GOWORK=off go build ./agentbase/...` | clean |
| Race-aware tests | `cd pkg && GOWORK=off go test -race ./agentbase/...` | ok 1.265s |
| Vet | `cd pkg && GOWORK=off go vet ./agentbase/...` | clean |
| Repo-wide lint | `make lint` | All 6 Go modules clean |
| Repo-wide tests | `make test` | All Go tests passed |
| Compile-time interface check (TokenResolver) | `rg '^var _ TokenResolver = \(\*tokenResolverImpl\)\(nil\)' pkg/agentbase/token_resolver.go` | 1 hit |
| Compile-time interface check (Dispatcher) | `rg '^var _ Dispatcher = \(\*dispatcherImpl\)\(nil\)' pkg/agentbase/dispatcher.go` | 1 hit |
| Constructor count | `NewTokenResolver=1, NewDispatcher=1, FuncClassifier=1` | all 1 |
| No speculative TokenResolver methods | `rg 'Refresh\(\|Invalidate\(\|IsExpired\(' pkg/agentbase/token_resolver.go` | 0 hits |
| Dispatcher methods | exactly `Dispatch`, `dedupeGate`, `dedupeStore` | 3 hits |
| File sizes | `wc -l pkg/agentbase/*.go` | max 374 (test); max 137 (source); all ≤500 |

## Deviations from plan

### Auto-fixed issues

**1. [Rule 1 — Bug] Plan referenced `tokenclient.Response`; actual type is `TokenResponse`**
- **Found during:** initial implementation
- **Issue:** Plan's pseudocode used `tokenclient.Response` as the type name; the actual exported type in `pkg/tokenclient/client.go` is `TokenResponse`.
- **Fix:** used the correct type name throughout `token_resolver.go` and the test.
- **Files modified:** `pkg/agentbase/token_resolver.go`, `pkg/agentbase/token_resolver_test.go`
- **Commit:** 7c26bb0

**2. [Rule 1 — Bug] Plan's pseudocode pre-encoded JSON before calling `dedupe.Store`**
- **Found during:** verification of `dedupeStore` against the agent-telegram handler.go
- **Issue:** Plan showed `d.dedupe.Store(ctx, ..., string(data))` after `json.Marshal`. But `hitldedupe.DedupeClient.Store(ctx, businessID, approvalID, result interface{})` accepts `interface{}` and json-marshals internally. The production handler in `services/agent-telegram/internal/agent/handler.go:126` passes `resp` directly.
- **Fix:** matched production behavior — pass `*a2a.ToolResponse` straight to `dedupe.Store`. Documented as D-AB-03 above so plan 19-07 doesn't reintroduce the redundant marshal.
- **Files modified:** `pkg/agentbase/dispatcher.go`
- **Commit:** 7c26bb0

**3. [Rule 1 — Bug] Plan's pseudocode for `dedupeGate` omitted the malformed-JSON `slog.WarnContext`**
- **Found during:** verification against handler.go
- **Issue:** The production handler logs `"hitl dedupe cached result malformed; returning generic duplicate"` when the cached blob fails to unmarshal; the plan's pseudocode silently returned the generic envelope.
- **Fix:** copied the slog call verbatim. Test `TestDispatch_DedupeGate_Duplicate_MalformedJSON_ReturnsGeneric` now asserts the canonical fallback envelope is returned without crashing.
- **Files modified:** `pkg/agentbase/dispatcher.go`
- **Commit:** 7c26bb0

### Lint clean-up after first golangci-lint run

- `errcheck` — `mr.Set(...)` return value not checked → wrapped in `require.NoError`
- `misspell` — "favour" → "favor", "honours" → "honors" (project's golangci-lint enforces US spelling)

These are not deviations from plan, just polish to satisfy the established repo-wide linter.

## What plan 19-07 will need to import

```go
import "github.com/f1xgun/onevoice/pkg/agentbase"

// In each agent's cmd/main.go (replacing the local tokenAdapter struct):
tokens := agentbase.NewTokenResolver(tokenClient)

// In each agent's cmd/main.go (replacing inline dedupe wiring + classifier closure):
classifier := agentbase.FuncClassifier(classifyTelegramError) // or VK / Yandex / GBP
dispatcher := agentbase.NewDispatcher(dedupe, classifier)

// In each agent's internal/agent/handler.go:
//   - struct field: dispatcher agentbase.Dispatcher
//   - constructor signature change: NewHandler(tokens agentbase.TokenResolver, factory SenderFactory, dispatcher agentbase.Dispatcher)
//   - Handle becomes:
func (h *Handler) Handle(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
    return h.dispatcher.Dispatch(ctx, req, h.routeTool)
}
//   - extract the tool switch into a private routeTool method
//   - delete local TokenInfo, TokenFetcher, dedupeGate, dedupeStore — replaced by agentbase types
```

After plan 19-07 completes, these acceptance checks must hold:

```bash
# Exactly one canonical TokenResolver impl
rg "func.*GetToken\(ctx context.Context, businessID, platform, externalID string\) \(.*TokenInfo, error\)" services/ pkg/
# expect: 1 hit (pkg/agentbase/token_resolver.go)

# No more dedupeGate methods on agent handlers
rg "func \(h \*Handler\) dedupeGate" services/agent-*/
# expect: 0 hits
```

## Self-Check

- [x] `pkg/agentbase/token_resolver.go` exists
- [x] `pkg/agentbase/dispatcher.go` exists
- [x] `pkg/agentbase/error_classifier.go` exists
- [x] `pkg/agentbase/token_resolver_test.go` exists
- [x] `pkg/agentbase/dispatcher_test.go` exists
- [x] Commit 7c26bb0 in `git log`
- [x] `cd pkg && GOWORK=off go test -race ./agentbase/...` exits 0
- [x] `make lint` exits 0 across all 6 Go modules
- [x] `make test` exits 0 across all 6 Go modules

## Self-Check: PASSED
