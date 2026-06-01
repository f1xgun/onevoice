# Search service

`services/api/internal/service/search.go` implements `Searcher` — the in-process two-phase Mongo search orchestrator behind `GET /api/v1/search`. Constructed once at startup; safe for concurrent reads.

## Two-phase query

1. **`ConversationRepository.SearchTitles`** — `$text` on `conversations.title` scoped by `(user_id, business_id, project_id?)`. Returns title hits.
2. **`ConversationRepository.ScopedConversationIDs`** — the broader allowlist for the next stage's `$in` filter (every conversation visible to the scope, not just title-matching ones).
3. **`MessageRepository.SearchByConversationIDs`** — `$text` on `messages.content` scoped by `conversation_id ∈ allowlist`.
4. **`mergeAndRank`** — fold per-conversation results, score `max(titleScore × titleHitWeight, contentScore × messageHitWeight)`, build snippet + highlight marks via the snowball-based helpers in `snippet.go`.

## Index strategy

Two `$text` indexes are created idempotently by `repository.EnsureSearchIndexes` (called from `wire.BootstrapDatabases`):

- `conversations_title_text_v19` — `default_language: russian`, weight 20.
- `messages_content_text_v19` — `default_language: russian`, weight 10.

The numeric weight ratio (2:1) must stay in step with the in-service ranking constants `titleHitWeight = 20.0` and `messageHitWeight = 10.0`. Changing one without the other breaks the contract that title hits outrank content hits of equal raw score at a 2:1 ratio.

### Readiness gate

`indexReady atomic.Bool` is set to `false` at construction. `MarkIndexesReady()` must be called from `cmd/main.go` AFTER `EnsureSearchIndexes` returns nil — the `atomic.Bool.Store` provides a happens-before edge against every subsequent `Load` by handler goroutines. Until the flag flips, `Search` returns `domain.ErrSearchIndexNotReady` and the handler maps the sentinel to HTTP 503 + `Retry-After: 5`.

**v20.1 note:** search no longer uses `$text` for the message scan (the messages stage now uses `SearchByConversationIDs` against a scoped allowlist), so a missing index can't surface `MongoServerError` any more. The flag is kept as a defensive "search service initialized" signal — if a future change reverts to `$text`, the gate is already wired and tested. Cost is negligible (one atomic load per request); benefit is a stable rollback path.

The flag is exposed via `IsReady()` (pure read, thread-safe) so health-check endpoints can report search readiness without reaching for an atomic primitive.

## Projection / response shape

```go
type SearchResult struct {
    ConversationID string     `json:"conversationId"`
    Title          string     `json:"title,omitempty"`
    ProjectID      *string    `json:"projectId,omitempty"`
    Snippet        string     `json:"snippet,omitempty"`
    MatchCount     int        `json:"matchCount"`
    TopMessageID   string     `json:"topMessageId,omitempty"`
    Score          float64    `json:"score"`
    Marks          [][2]int   `json:"marks,omitempty"`
    LastMessageAt  *time.Time `json:"lastMessageAt,omitempty"`
}
```

JSON tags drive the `GET /api/v1/search` response shape directly. `Marks` is the highlight-range pair list `[start, end]` produced by `HighlightRanges` over the snippet text. `Snippet` is built by `BuildSnippet` against the top-scoring content message; when only a title hit exists, the snippet is empty.

## Business-scope (cross-tenant defense)

Defense-in-depth: `Search` returns `domain.ErrInvalidScope` immediately when `businessID == ""` OR `userID == ""`, with no repo calls. The repository-layer methods independently enforce the same guard, so any caller path that smuggles an empty scope is blocked twice. The allowlist used by the content-search stage comes from `ScopedConversationIDs`, which itself enforces scope at the Mongo filter level.

## Log shape (no query-text leak)

Every `slog.InfoContext` line carries `query_length` only — NEVER the literal query text. The single `search.query` log line emits `{user_id, business_id, query_length}`. No `"query"` slog key appears anywhere in this file. Verified by `TestSearcher_LogShape_NoQueryText`.

## Ranking

`mergeAndRank(titleHits, msgHits, titleW, contentW, limit, prefixes)`:

- Score formula per conversation: `max(titleScore × titleW, contentScore × contentW)`.
- When both title and content hit, snippet/marks/match_count are taken from the content side; score keeps the stronger of the two weighted scores.
- Final sort uses `sort.SliceStable` with a fully-ordered comparator:
  1. score desc
  2. `last_message_at` desc (recency)
  3. `conversation_id` asc (deterministic tiebreaker)

**Why the tertiary keys matter.** Without them, equal-scoring rows ride on Go's non-deterministic map iteration order through `sort.Slice` — so the SAME query could return rows in different orders across calls. `SliceStable` + the fully-ordered comparator removes the wobble.

Default `limit = 20` when caller passes ≤ 0. Content stage requests `limit * 2` so the merge has headroom for title-only hits before truncation.

## Constructor contract

`NewSearcher(convRepo, msgRepo)` panics loudly on nil deps — startup-time wiring bugs must surface at the bind site, not at the first failed search. Pattern parallels `service.NewTitler`.
