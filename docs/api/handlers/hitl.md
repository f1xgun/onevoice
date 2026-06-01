# HITL handler

`services/api/internal/handler/hitl.go` ships every HITL HTTP surface on the
API service:

```
POST /api/v1/conversations/{id}/pending-tool-calls/{batch_id}/resolve
POST /api/v1/chat/{id}/resume?batch_id=X
GET  /api/v1/tools
```

The business-logic layer lives in `services/api/internal/service/hitl.go`
(atomic transition, TOCTOU re-check, edit validation). The handler is the
HTTP adapter: parses path/body/query, calls into `HITLService`, maps
service-layer errors to HTTP status codes, and SSE-proxies the resume call
to the orchestrator.

## Construction

`NewHITLHandler` takes three required collaborators:

- `*service.HITLService` — owns the resolve transition and the
  pending/project repo accessors.
- `BusinessService` — used on the resume path to translate
  `bc.BusinessID` into a full `domain.Business` so the orchestrator's
  TOCTOU re-check sees the actor's current `ToolApprovals` map.
- `domain.ConversationRepository` — retained on the struct for legacy
  parity with the original five-arg shape; resume now reads ownership
  from the batch row (`business_id`, `conversation_id`) directly, so the
  repo is intentionally unused inside `Resume` itself.

Nil deps return a `fmt.Errorf` from the constructor — the wire package
fails loud at startup rather than tripping a nil-pointer dereference on
the first HITL request.

## ACL checks (Resolve)

1. `BusinessContextFromCtx` — 500 if the middleware chain skipped
   `RequireBusinessAccess`. Belt-and-braces backstop; `lint-rbac` catches
   regressions at PR time.
2. `authz.Can(PermContentUpdate)` — 403 if missing. Approving a pending
   tool-call IS a content write (the tool will mutate platform state on
   dispatch), so the same permission gates both the chat write path and
   the HITL decision.
3. **Cross-tenant defense lives in the service layer.** The handler
   passes `bc.UserID` and `bc.BusinessID` into `service.ResolveInput`;
   `HITLService.Resolve` uses both to compose the ownership predicate
   on the pending-batch row. A spoofed `batch_id` from another business
   surfaces as `ErrHITLBatchNotFound` → uniform 404 (NEVER 403 — uniform
   404 is the industry-standard guard against batch-id enumeration).

## ACL checks (Resume)

1. `BusinessContextFromCtx` — 500 backstop as above.
2. `authz.Can(PermContentCreate)` — 403 if missing. Resume RE-DRIVES the
   chat turn after approval and the LLM may emit further content, so
   the gate matches the chat-proxy's create permission, NOT the update
   permission used by Resolve.
3. `bc.BusinessID` cross-checked against `batch.BusinessID` and the URL
   `conversation_id` cross-checked against `batch.ConversationID`. Both
   mismatches return 403 — the batch has already been looked up by the
   time we reach these checks, so existence has leaked anyway and the
   uniform-404 invariant does not apply here.

## ACL checks (GetTools)

Tools registry is the same for every business. Auth is enforced by the
middleware chain (a valid JWT); the handler does NOT apply
`RequireBusinessAccess` or a permission check. Listing the catalog of
known tool names + floors + editable-fields is treated as a directory
read, not a per-business resource read.

## Error mapping (Resolve)

`mapResolveError` is the centralised translation table. The happy-path
handler stays linear by delegating every non-nil error to this single
switch. Mappings:

| Service-layer error                     | HTTP | Body                                                               |
|-----------------------------------------|------|--------------------------------------------------------------------|
| `ErrHITLBatchNotFound`                  | 404  | `{"error":"batch not found"}`                                      |
| `ErrHITLForbidden`                      | 403  | `{"error":"forbidden"}`                                            |
| `ErrHITLBatchExpired`                   | 410  | `{"error":"approval_expired"}`                                     |
| `ErrHITLDecisionsShape`                 | 400  | `{"error":"shape mismatch","missing":[...]}`                       |
| `*tools.ErrFieldNotEditable`            | 400  | `{"error":"field … not editable for tool …","editable":[...]}`     |
| `*tools.ErrNonScalarValue`              | 400  | `{"error":"field … must be string/number/bool","tool":"…"}`        |
| `ErrHITLRejectReasonTooLong`            | 400  | `{"error":"reject_reason too long","max":N}`                       |
| `ErrHITLBatchAlreadyResolving`          | 409  | `{"error":"batch resolving","retry_after_ms":500,"reason":"concurrent resolve in progress"}` |
| anything else                           | 500  | `{"error":"internal server error"}` + slog at ERROR with `error`   |

`hitlBatchResolvingRetryAfterMs = 500` balances "don't hammer" against
"feels responsive" for chat-y interactions. The frontend treats the 409
+ `retry_after_ms` envelope as transient and replays automatically.

`nilToEmptyStringArr` exists because `*tools.ErrFieldNotEditable` may
carry a nil `Editable` slice (tool has no editable fields at all). The
frontend's zod schema requires `editable: string[]` (non-null), so the
mapper emits `[]` rather than `null`.

## Pinning (Resolve)

The handler NEVER reads `tool_name` from the request body. The service
layer always uses `c.ToolName` from the persisted `PendingCall` row.
Any caller-supplied `tool_name` field in `Decisions[].Edits` is rejected
by `ValidateEditArgs` because `tool_name` is not in any tool's
`EditableFields` allowlist — the handler does not need its own filter.

## Idempotence / atomic transition (Resolve)

The race-safe primitive is in the service layer:

1. Service `Resolve` opens a transaction and performs a conditional
   UPDATE on the batch row pinned to `(batch_id, status='pending')`.
2. `RowsAffected = 0` means another caller has already won — the repo
   re-reads the row, classifies the terminal status, and returns the
   matching sentinel (`ErrHITLBatchExpired` for expired,
   `ErrHITLBatchAlreadyResolving` for already-in-flight).
3. The handler maps those sentinels per the table above; the second
   caller sees 409 / 410, never a partial double-dispatch.

## Resume lifecycle

`Resume` validates ownership and status, builds a fresh tool-approval
view, then SSE-proxies the orchestrator's `/chat/{id}/resume`:

1. Load the actor's business via `BusinessService.GetByID`. Missing →
   404 (matches the chat-proxy missing-business contract).
2. Load the pending batch via `HITLService.PendingRepo().GetByBatchID`.
   Missing → 404. Repo errors → 500.
3. Cross-check `batch.BusinessID == business.ID` and
   `batch.ConversationID == URL conversation_id`. Either mismatch → 403.
4. Reject batches in `status='expired'` with 410
   `{"error":"approval_expired"}`.
5. Reject batches in `status='resolved'` with 409
   `{"error":"batch already resolved","reason":"already_resolved"}`. The
   handler intentionally does NOT reject `status='resolving'` — see the
   "Resume vs resolving" section below.
6. Build the fresh maps `business_approvals` (from `business.ToolApprovals()`)
   and `project_approval_overrides` (from
   `HITLService.ProjectRepo().GetByID` keyed by `batch.ProjectID`). Project
   lookup failures are swallowed: the resume body still carries the
   business-level map and the orchestrator's TOCTOU re-check degrades to
   "business-only" rather than failing the resume.
7. Marshal `{business_approvals, project_approval_overrides}` and call
   `orchestratorclient.StreamSSE` which owns URL selection (`batch_id` →
   resume), correlation-id propagation, SSE response headers, the scanner
   with a 1 MiB buffer, and the raw-forward drain loop.
8. Pre-connect failures wrap their error with `"stream resume:"`. After
   `StreamSSE` writes the SSE response headers, callers cannot emit a
   JSON error body — so the handler maps ONLY the pre-connect substring
   to 502. Mid-drain failures are logged at WARN and the in-flight 200
   SSE response is left committed as-is.

### Resume vs `status='resolving'`

A previous revision rejected `status='resolving'` with 409 here. That
was wrong:

- `Resolve` atomically transitions `pending → resolving`.
- The orchestrator's resume goroutine is the ONLY writer that transitions
  `resolving → resolved` (via `MarkResolved` after dispatch completes).
- So the legitimate first resume call ALWAYS finds the batch in
  `resolving`. Rejecting it bricked every approval flow once the 403
  short-circuit was lifted.

The truly-conflicting terminal state is `resolved` (already-dispatched);
`expired` is handled above as 410. Per-call double-dispatch protection
lives in the orchestrator's `MarkDispatched` idempotence guard, not here.

## SSE proxy ownership

`StreamSSE` (from `pkg/orchestratorclient`) is the single owner of:

- URL selection — `batch_id` non-empty → `/chat/{id}/resume?batch_id=X`,
  otherwise `/chat/{id}`.
- Correlation-id propagation from `r.Context()`.
- SSE response headers (`Content-Type: text/event-stream`,
  `Cache-Control: no-cache`, `Connection: keep-alive`).
- Scanner buffer (1 MiB) — large enough for a fully-formed tool-call
  event without forcing the frontend into chunked-reassembly.
- The raw-forward drain loop — every byte the orchestrator emits goes
  through to the client without re-parsing.

`OnEvent` is `nil` from this handler — no per-event domain dispatch is
needed because the chat-proxy already handles persistence of the
orchestrator stream during the FIRST turn; resume only re-drives a
tail of the same stream.

## GetTools projection

The endpoint returns the live orchestrator registry projection via the
`HITLService.ToolsCache().List` accessor:

- 5-minute TTL inside the cache.
- Empty `EditableFields` is normalised to `[]` so downstream consumers
  never see `null` (the frontend's React Query cache + zod schema both
  expect `string[]`).
- The frontend re-caches the response client-side so settings pages +
  the project edit page share a single in-browser copy.
