# `pkg/hitlstore` — Pending Tool-Call Storage

Mongo-backed implementation of `domain.PendingToolCallRepository`, shared by
`services/api` (resolve handler, reconciliation sweep, `ListPending`) and
`services/orchestrator` (pause-time persistence, resume-time
`MarkDispatched`).

## Deployment constraint

MongoDB is deployed STANDALONE (`docker-compose.yml` uses `mongo:7` without
`--replSet`). **No multi-document transactions.** Atomicity is achieved via
`findOneAndUpdate` filter constraints, NOT session-scoped transaction APIs.

Do NOT introduce session-scoped code into this package — it will panic at
runtime on standalone deployments.

## State machine

```
            Persist (stage)              Persist (promote)
   (none) ──────────────────▶ preparing ─────────────────▶ pending
                                  │                          │
                                  │ReconcileOrphanPreparing  │AtomicTransitionToResolving
                                  ▼                          ▼
                               expired                   resolving
                                                             │
                                                             │MarkResolved / MarkExpired
                                                             ▼
                                                      resolved / expired
```

Owned transitions:
- **Persist** — stages `preparing`, promotes to `pending`.
- **AtomicTransitionToResolving** — `pending` → `resolving`.
- **RecordDecisions** — writes per-call verdicts (status unchanged).
- **MarkDispatched** — positional update on `calls.$.dispatched` (status
  unchanged).
- **MarkResolved** / **MarkExpired** — terminal flips.
- **ReconcileOrphanPreparing** — crash-recovery sweep `preparing` → `expired`.

Reads:
- **GetByBatchID** — single doc with lazy TTL virtualization.
- **ListPendingByConversation** — open batches sorted oldest-first.

## Atomicity discipline

`AtomicTransitionToResolving` is the **one atomicity primitive**:
`findOneAndUpdate` with filter `{_id, status: "pending"}` guarantees at most
one winner across arbitrarily many racing resolve calls. Mongo serializes
the update at the document level — only the first matching update returns
the post-update doc; every subsequent call falls into `mongo.ErrNoDocuments`.

**The filter constraint IS the serialization** — any refactor must preserve
it exactly. No session-scoped transactional APIs.

The two-step disambiguation (re-`FindOne` on `ErrNoDocuments`) exists so the
resolve handler can return 404 (true miss / `ErrBatchNotFound`) vs 409
(concurrent resolve or already-terminal / `ErrBatchNotPending`). Without the
second lookup the caller could not tell them apart.

## Persist write-order invariant

`Persist` stages `preparing` and promotes to `pending` with a 24h TTL.
From the caller's view the batch transitions atomically from non-existent
to pending; the preparing window is an implementation detail.

Why two steps:

1. **TTL safety.** The TTL index keys on `expires_at`. If the row were
   inserted directly with `expires_at` set, a crash before the orchestrator
   finished emitting the SSE event would leave a fully-pending row exposed
   to TTL deletion at an arbitrary time before any user-visible interaction.
   The preparing window holds `expires_at` unset so the TTL sweep ignores
   stillborn rows. `ReconcileOrphanPreparing` is the deterministic reaper.

2. **Crash recovery.** A crash strictly between the `InsertOne` and the
   promotion `UpdateOne` leaves the row in `preparing`. The reconcile sweep
   filter `{status: "preparing", created_at < cutoff}` picks it up at the
   next API startup and flips it to `expired`.

If promotion sees nothing in `preparing`, the only path that produces this
is a reconcile sweep flipping the batch to `expired` between the two
writes. Surface this unusual case as `ErrBatchNotFound` so the caller fails
loudly rather than emitting a pause event for a non-existent batch.

### Identity guard

`ConversationID` and `BusinessID` are the structural floor — every
downstream path (pending-batch hydration filter, resolve-time
business-scoped auth check) depends on both being non-empty. Earlier code
paths persisted empty IDs and broke both paths silently; Persist fails
loud so a future regression of `chat.go` / `chat_proxy.go` cannot silently
write empty IDs again.

`UserID` and `MessageID` are intentionally NOT guarded — system / anonymous
flows may legitimately have an empty `UserID`.

## TTL and indexes

`EnsurePendingToolCallsIndexes` creates three indexes idempotently:

| Name | Keys | Purpose |
|---|---|---|
| `pending_tool_calls_ttl` | `{expires_at: 1}` | `expireAfterSeconds=0` — docs expire at their own `expires_at` (up to 60s lag). Stillborn `preparing` rows have no `expires_at` so TTL skips them; `ReconcileOrphanPreparing` reaps them instead. |
| `pending_tool_calls_conv_status` | `{conversation_id: 1, status: 1}` | Supports `ListPendingByConversation`'s typical predicate. |
| `pending_tool_calls_business` | `{business_id: 1}` | Future business-scoped dashboards / metrics queries. |

Safe to call on every boot — `CreateMany` silently succeeds when specs
match existing indexes. Duplicate-key errors are swallowed.

Only the API service calls this at boot — the orchestrator does not own
schema bootstrap.

## Lazy expiration

`GetByBatchID` virtualizes `Status` to `"expired"` when `status == "pending"`
but `expires_at` has already passed. This covers the up-to-60s window where
the TTL sweep has not yet fired but the document is logically expired.
Callers never see a stale `"pending"` past the 24h window.

## `ListPendingByConversation`

Returns every batch for the conversation whose status is `pending` OR
`resolving`, sorted oldest-first. Resolved / expired / preparing batches
are filtered out — callers needing those use `GetByBatchID` directly.

## `MarkDispatched`

Flips `calls.$.dispatched=true` + `calls.$.dispatched_at=now` for the
matching `call_id` via Mongo's positional `$` operator. The filter
includes `"calls.call_id"` so the update only runs when the batch actually
contains the given call. **Missing batch/call combinations are silent
no-ops** — intentional for the resume-recovery flow where calls are
optimistically marked after a NATS reply lands.

## `ReconcileOrphanPreparing`

Sweeps batches stuck in `status="preparing"` whose `created_at` is older
than `olderThan`, marking them expired. Called once at API startup
(`services/api/cmd/main.go`) to clean up crashes where the orchestrator
inserted a preparing row but never got to promote.

Returns rows transitioned. Safe to re-run — idempotent by filter
(already-expired rows don't match `status=preparing`).

## Constants

- `pendingToolCallTTL = 24h` — how long an approval batch stays pending
  before lazy expiration. Gives users a full business-day window to act
  on an approval card.

## Cross-references

- `docs/services/hitl.md` — resolve service contract, error semantics,
  registry cache.
- `pkg/domain/repository.go` — `PendingToolCallRepository` interface,
  `PendingToolCallBatch` / `PendingCall` shapes, sentinel errors.
- `services/orchestrator/internal/resume` — load-bearing TOCTOU recheck on
  the resume goroutine.
