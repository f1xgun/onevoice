# Conversation repository (MongoDB)

`services/api/internal/repository/conversation.go` owns every Mongo write against the `conversations` collection. Conversations are MongoDB documents (not Postgres rows) because the variable-shape title state, project assignment, and pin metadata travel with the conversation but don't participate in cross-table joins.

## Document shape and projection discipline

The Mongo document mirrors `domain.Conversation`. Two fields are nullable but **must not carry `omitempty`** on their BSON tag, and this repo's writes rely on that:

- `project_id` — `UpdateProjectAssignment` writes `nil` so the field stores BSON `null` (move-chat clearing project). Without an explicit `null`-vs-missing distinction, the sidebar's per-project query and the audit-trail of "moved out of project" would be ambiguous.
- `title_status` — legacy rows from before the auto-titler shipped have no value at all. `UpdateTitleIfPending` uses `{$in: [auto_pending, null]}` so the same atomic write matches both legacy-null and explicitly-pending rows. If `title_status` carried `omitempty`, drivers might serialise null as "missing field" and the `$in` match would break across driver versions.

Both invariants are domain-layer concerns enforced at the BSON-tag level; this repo only consumes them.

## Write-order discipline and idempotence

The repo distinguishes three kinds of writes:

1. **Unconditional updates** (`Update`, `Delete`, `UpdateProjectAssignment`) — set the row, return `ErrConversationNotFound` on zero matches.
2. **Atomic conditional writes** (`UpdateTitleIfPending`, `TransitionToAutoPending`, `Pin`, `Unpin`) — the filter encodes a precondition (status, scope tuple). Zero matches is the *intended* signal to the caller; the caller treats it as "the precondition failed" and adjusts.
3. **Bulk hard-delete sweepers** (`MongoConversationsCleanup`) — multi-row updates that run after a Postgres tx commits and cannot themselves be transactional with Postgres.

`Create` is the only write that touches `created_at`. `Update` deliberately omits `created_at` so callers cannot accidentally clobber the creation timestamp during a rename.

`Update` persists `title_status` because handler-level rename flips status to `"manual"` and that flip is trust-critical: a concurrent in-flight titler must NOT clobber a manual rename. If `Update` quietly dropped the field, the durability of the manual-rename contract would be lost at the repo layer.

## Sovereign manual renames vs. auto-titler

`UpdateTitleIfPending` and `TransitionToAutoPending` are the two atomic conditional writes that defend the auto-titler race:

- `UpdateTitleIfPending` filters `{_id, title_status: {$in: [auto_pending, null]}}`. A manual rename has flipped status to `"manual"`, so the titler's eventual write matches zero documents and surfaces as `ErrConversationNotFound`. The handler maps that to a silent no-op — the manual rename wins.
- `TransitionToAutoPending` (POST `/regenerate-title`) filters `{_id, title_status: {$in: [auto, auto_pending, null]}}`. `manual` is excluded (sovereign). `auto_pending` is *included* in the filter so the recovery path for a stuck-pending row (older than the handler's 30s grace window) is a deterministic no-op-then-bump rather than a confusing not-found-shaped 409.

Both methods bump `updated_at` so the handler's grace window can detect "this row was touched recently" without joining against an external clock.

## Business-scope isolation (Pin/Unpin)

`Pin` and `Unpin` filter on the full `(_id, business_id, user_id)` tuple even though `_id` alone is unique. The defence-in-depth `business_id + user_id` clause prevents cross-tenant pin manipulation if a caller misroutes IDs. On zero matches the methods return `ErrConversationNotFound`, which the handler maps to uniform HTTP 404 — never 403. Uniform 404 vs. ownership-aware 403 is the industry-standard guard against existence enumeration.

## Account-deletion cleanup

`MongoConversationsCleanup` is the hard-delete sweeper for soft-deleted users. It runs **after** the Postgres tx commits — Mongo does not participate in the PG tx, so this is best-effort by construction. The caller logs a warning on failure but does NOT roll back the PG delete; the PG row is already gone.

The sweeper does NOT delete the conversation document. It sets `user_id = null`, snapshots the user's original email under `user_email_at_delete`, and stamps `deleted_owner = true`. Rationale: business-level history must survive when an individual user disappears (152-ФЗ / GDPR compromise). The document continues to be reachable via business-scoped queries; only the user-scoped queries no longer return it.

## Index strategy

`EnsureConversationIndexes` is the idempotent boot-time index manager. Two named compound indexes are owned here:

1. `conversations_user_biz_title_status` — `{user_id, business_id, title_status}`. Backs the auto-titler's `UpdateTitleIfPending` lookups and the sidebar's "rows in auto_pending" queries. **Do not modify** — index shape is hot-pathed by the titler and any rename forces an online reindex.
2. `conversations_user_biz_proj_pinned_recency` — `{user_id, business_id, project_id, pinned_at:-1, last_message_at:-1}`. ESR layout: equality on `(user, business, project)`, descending sort on `(pinned_at, last_message_at)`. Backs the sidebar's "pinned-then-recent per project" sort so it's index-served.

`CreateMany` silently succeeds when specs match existing indexes; the duplicate-key check exists defensively though name-conflict is the more likely failure mode (stable named-index specs across boots).

## Search projections

Two methods support the two-phase search strategy:

- `SearchTitles` runs an AND-of-prefixes regex query against `conversations.title` scoped by `(user_id, business_id, project_id?)`. Returns the title hits AND the slice of conversation IDs that matched. Why regex over `$text`: Mongo's Russian Snowball stems had asymmetric recall (see `message.go::SearchByConversationIDs`). v20.1's per-token word-prefix regex gives morphological recall over inflectional suffixes without the asymmetry. `hit.Score` is set to `1.0` (per matching conversation, since each title is a single string); the downstream merge in `service.Searcher` applies `titleHitWeight` to bias title hits over content hits.
- `ScopedConversationIDs` is phase 1 of the two-phase strategy: returns every conversation ID visible to `(user_id, business_id, project_id?)`, ordered by `last_message_at DESC`, capped at `MaxScopedConversations + 1` so the caller can detect overflow. The caller feeds the slice into `messageRepository.SearchByConversationIDs` as the cross-tenant allowlist for phase 2.

`MaxScopedConversations = 1000` bounds query cost and Mongo's `$in` size on future paths. Overflow above the cap is logged with metadata-only fields (never the query, never the IDs) and the slice is truncated to the most-recently-active 1000.

Defence-in-depth: both methods return `domain.ErrInvalidScope` immediately when `businessID == ""` or `userID == ""`. The repo-level guard parallels the service-layer guard so a cross-tenant leak cannot happen even if a future caller forgets to scope. Empty / whitespace-only `SearchTitles` query returns `([], nil, nil)` — the handler already enforces `len(q) >= 2`.

## Cross-references

- [docs/architecture.md](../../architecture.md)
- [docs/services/conversation.md](../../services/conversation.md) — service-layer composition (`MoveToProject`, `OpenChat`).
- [docs/services/titler.md](../../services/titler.md) — handler-level rename grace window and sovereign-rename contract.

An empty title passed to `UpdateTitleIfPending` is the terminal fallback marker.
The atomic write persists both `title: ""` and `title_status: "auto"`; no
localized text or historical rewrite is needed. Nonempty model titles use the
same generated status. Search title projections preserve title status and
creation time so clients can localize fallback display without altering storage.
