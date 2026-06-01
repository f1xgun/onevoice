# HITL (Human-In-The-Loop) Service

`services/api/internal/service/hitl.go` implements the core atomic-resolve path
behind `POST /api/v1/conversations/{id}/pending-tool-calls/{batch_id}/resolve`.
It enforces every HITL safety property at the business-logic layer so that the
HTTP handler can stay a thin parse/format facade.

The companion `ToolsRegistryCache` declared in the same file is a thin
in-memory cache over `GET /internal/tools` on the orchestrator. The resolve
flow reads `EditableFields` from it on the hot path; settings pages also read
it via `GET /api/v1/tools`.

## Public API

- `func NewHITLService(pendingRepo, businessRepo, projectRepo, toolsCache, orch) *HITLService` —
  Constructor. All five dependencies are mandatory; a nil anywhere indicates
  a wiring bug and panics at construction time.
- `func (s *HITLService) PendingRepo() domain.PendingToolCallRepository` —
  Exposes the pending-tool-call repo so the resume-path handler can perform
  pre-flight status checks (e.g. 409 when the batch is resolving).
- `func (s *HITLService) BusinessRepo() domain.BusinessRepository` —
  Exposes the business repo so the resume handler can re-fetch FRESH
  `tool_approvals` before forwarding to the orchestrator.
- `func (s *HITLService) ProjectRepo() domain.ProjectRepository` —
  Same fresh-fetch rationale for project approval overrides.
- `func (s *HITLService) ToolsCache() *ToolsRegistryCache` —
  Shared tools-registry cache. Handlers outside this file
  (`GET /api/v1/tools`, PUT approvals) share the same instance.
- `func (s *HITLService) OrchClient() *orchestratorclient.Client` —
  Shared orchestrator HTTP client. Callers invoke `StreamResume` through it
  without re-implementing the HTTP plumbing.
- `func (s *HITLService) Resolve(ctx, ResolveInput) (*ResolveResult, error)` —
  Atomic resolve entry point. See lifecycle below.
- `func NewToolsRegistryCache(orchestratorURL, httpClient, ttl) *ToolsRegistryCache` —
  Constructor for the registry cache. `ttl` defaults to 5 minutes when
  non-positive. `httpClient` nil falls back to `http.DefaultClient`.
- `func (c *ToolsRegistryCache) Seed(entries)` —
  Pre-populates the cache with a static snapshot so callers (typically
  tests) avoid HTTP round-trips against the orchestrator. Populates every
  supported locale tag (`""`, `"ru"`, `"en"`) with the same data.
- `func (c *ToolsRegistryCache) List(ctx) []ToolsRegistryEntry` —
  Returns cached entries for the locale carried on `ctx`
  (`i18n.LocaleFromContext`). Refreshes on per-locale TTL expiry.
- `func (c *ToolsRegistryCache) Floor(toolName) domain.ToolFloor` —
  Returns the floor for the named tool, or `Forbidden` if unknown.
- `func (c *ToolsRegistryCache) EditableFields(toolName) []string` —
  Per-tool edit allowlist, or nil for unknown tools. nil means
  "every edit on this tool is rejected".
- `func (c *ToolsRegistryCache) Has(toolName) bool` —
  Reports whether the tool name is currently cached.

## Business rules (resolve path)

The resolve endpoint enforces these properties at the service layer; the
handler layer (`handler/hitl.go`) does HTTP status mapping only.

- **Ownership** — `actor.BusinessID == batch.BusinessID` and
  `actor.ConversationID == batch.ConversationID`. Cross-tenant access → 403.
- **Decision shape** — exactly one decision per pending call. Bad shape →
  400 with `{missing: [...]}` body. The `missing` slice carries call_ids
  not covered by the submitted decisions.
- **Edit validation** — `pkg/tools.ValidateEditArgs` enforces the per-tool
  `EditableFields` allowlist. Invalid field → 400 with `{editable: [...]}`.
- **Reject reason cap** — `reject_reason` is bounded at
  `MaxRejectReasonChars` (500). The frontend textarea enforces the same
  limit.
- **Atomic status transition** — `AtomicTransitionToResolving`
  (`findOneAndUpdate` with filter `{_id, status: "pending"}`) guarantees
  exactly-one-wins on concurrent resolve attempts. Loser gets
  `ErrBatchNotPending` → 409 `{retry_after_ms}`.
- **TOCTOU re-check** — `pkg/hitl.Resolve` runs against FRESH
  business/project approval maps. Forbidden-after-pause flips to a
  synthetic `"policy_revoked"` rejection; the API still returns 200 and
  the LLM sees the synthetic rejection on resume.
- **tool_name pinning** — the server NEVER reads `tool_name` from the
  request body. It always pulls from the persisted `PendingCall` row so
  the client cannot misdirect validation against a different tool.

## Resolve lifecycle (order-sensitive)

1. Load batch — ownership + existence check.
2. Validate decisions shape (`missingCallIDs`) — exactly one per call.
3. Edit validation per call. `tool_name` is read from the persisted
   `PendingCall`, never from the client. Reject reason length checked
   here too.
4. Atomic status transition to `"resolving"` — 409 on the concurrent
   resolve race (anti-footgun).
5. TOCTOU re-check with FRESH business/project maps.
   `Resolve` uses the `FloorAtPause` persisted by the orchestrator
   rather than `s.toolsCache.Floor`. Rationale: the api-side
   `ToolsRegistryCache` is HTTP-backed and lazily warmed (only
   `GET /api/v1/tools` triggers refresh), so it would spuriously
   return `Forbidden` for valid manual-floor tools whenever the
   settings page had not been visited in this api process lifetime.
   The persisted `FloorAtPause` is the faithful echo of the
   orchestrator's in-process `tools.Registry` (always warm) at the
   moment of pause; the orchestrator-side resume goroutine
   (`resume.go: dispatchApprovedCalls`) remains the load-bearing
   TOCTOU recheck.
6. Persist final per-call verdicts via `RecordDecisions`.

The response is plain JSON. The client separately opens
`/chat/{id}/resume` to obtain the SSE continuation stream.

## Error semantics

Each typed error in this file maps to a specific HTTP status code in
`handler/hitl.go`.

| Error | Status | Meaning |
|---|---|---|
| `ErrHITLBatchNotFound` | 404 | `batch_id` not in the collection |
| `ErrHITLForbidden` | 403 | Actor's business does not own the batch |
| `ErrHITLBatchExpired` | 410 | Batch passed its TTL window |
| `ErrHITLBatchAlreadyResolving` | 409 | Concurrent resolve won the race |
| `*ErrHITLDecisionsShape` | 400 | `missing` slice echoed in the response body |
| `*ErrHITLRejectReasonTooLong` | 400 | `reject_reason` exceeds `MaxRejectReasonChars` |

`missingCallIDs` does not separately flag extra decisions: by pigeonhole, an
extra decision whose ID matches no call in the batch implicitly leaves some
real call uncovered. The strict-shape branch (`len(missing) == 0 &&
len(decisions) != len(calls)`) returns nil because the duplicate-ID case is
absorbed by `decisionByID` map overwrite — the batch's call_ids are unique.

## ToolsRegistryCache

A 5-minute-TTL cache over the orchestrator's tool registry projection
(`name, platform, floor, editable_fields, description`). Stays a map lookup
on the resolve hot path.

- **Concurrency** — `sync.RWMutex` guards both entries and the
  refresh-in-flight guard. On TTL expiration the first reader triggers a
  single refresh; concurrent readers wait on the in-flight channel and
  observe the refreshed entries.
- **Per-locale snapshots** — `byLocale` stores one snapshot per
  language tag keyed by `tag.String()` (`"ru"`, `"en"`, `""`). Each
  snapshot tracks its own `loadedAt` so the TTL works per-locale.
- **`latestEntries`** is the most recently loaded snapshot, used for
  the locale-agnostic `Floor` / `EditableFields` / `Has` lookups.
  Floor data is identical across locales so any locale's entries are
  equivalent for those callers.
- **Stale-on-error** — stale entries are preferred over empty results
  to avoid cascading 500s.
- **First-ever load failure** — cache returns empty. Every tool then
  has nil `EditableFields`, which causes edit validation to reject
  every field as not-editable (safe default: fail-closed).
- **Per-tool lookup** — `Floor` / `EditableFields` / `Has` use a
  linear scan rather than a preallocated map. With ~20-30 tools in
  v1.3 the loop beats a map's pointer chase and GC pressure.
- **Test mode** — `Seed` populates the cache directly; if
  `orchestratorURL` was empty at construction time, `c.orch` is nil
  and `refresh` returns immediately so unit tests never reach the
  network.

`ToolsRegistryEntry` is aliased to `domain.ToolEntry` so the orchestrator
and the API share one canonical type instead of redefining the same JSON
shape on both sides of the `internal/` visibility wall. Field
documentation lives in `pkg/domain/tool_entry.go`.

## Cross-references

- `pkg/hitl` — pause-time floor evaluation primitives (`Resolve`).
- `pkg/tools` — `ValidateEditArgs`, per-tool editable field enforcement.
- `pkg/orchestratorclient` — HTTP client for `GET /internal/tools` and
  resume stream.
- `services/orchestrator/internal/resume` — load-bearing TOCTOU recheck on
  the resume goroutine.
- `docs/architecture.md` — top-level system flow.
- `docs/api-design.md` — REST conventions and shared error shape.
