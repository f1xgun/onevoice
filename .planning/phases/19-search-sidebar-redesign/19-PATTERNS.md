# Phase 19: Search & Sidebar Redesign — Pattern Map

**Mapped:** 2026-04-27
**Files analyzed:** 30 files (19 backend + frontend deliverables, 4 cross-cutting test scaffolds, 7 dependency / config / reuse-only entries)
**Analogs found:** 27 strong analogs / 30 files (3 entries are dependency-only or "reuse without modification")

This map locks per-file pattern assignments for the Phase 19 planner. Every excerpt below is from a real file in this repo at the cited line numbers; copy the shape, do not invent. Cross-cutting conventions (handler→service→repository layering, slog metadata-only, atomic Mongo conditional updates, Russian UI copy, t.Setenv, idempotent CreateMany indexes) are listed once in **Shared Patterns** and referenced from the per-file rows.

---

## File Classification

| # | New / Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|--------------------|------|-----------|----------------|---------------|
| 1 | `pkg/domain/mongo_models.go` | domain | pure-render (struct shape) | same file — `Conversation` struct lines 39–50 | exact (extends existing struct) |
| 2 | `services/api/internal/repository/conversation.go` (extend `EnsureConversationIndexes` + add `SearchTitles` + add `Pin`/`Unpin`) | repo | read-only (search) + read-write (pin) | same file — `EnsureConversationIndexes` lines 224–243; `UpdateTitleIfPending` lines 155–177 (pin atomic write) | exact |
| 3 | `services/api/internal/repository/message.go` (add `SearchByConversationIDs`) | repo | read-only | same file — `FindByConversationActive` lines 95–115 (filter+aggregate shape); RESEARCH §5 pipeline | role-match (no aggregation precedent in this repo) |
| 4 | `services/api/internal/repository/mongo_backfill.go` (add `BackfillConversationsV19`) | repo (migration) | write-only | same file — `BackfillConversationsV15` lines 35–100 | exact |
| 5 | `services/api/internal/service/search.go` (NEW) | service | read-only (orchestrating) | `services/api/internal/service/titler.go` lines 60–243 (constructor guards + slog metadata + multi-step pipeline) | role-match (no fan-out service over multiple repos exists yet) |
| 6 | `services/api/internal/service/snippet.go` (NEW; sibling of search.go for `BuildSnippet` + `HighlightRanges`) | service (pure helper) | pure-render | `services/api/internal/service/titler.go` `sanitizeTitle`/`untitledChatRussian` lines 251–273 (pure rune/byte-aware helpers) | role-match |
| 7 | `services/api/internal/handler/search.go` (NEW) | handler | request-response | `services/api/internal/handler/conversation.go` `ListConversations` lines 202–240 + `GetConversation` lines 243–284 (auth + parse query + writeJSON) | exact |
| 8 | `services/api/cmd/main.go` (extend index/backfill block; wire searcher; register route) | config / wiring | request-response (registration) | same file — Phase 18 backfill+index block lines 88–130; HITL wiring lines 365–377 | exact |
| 9 | Search-readiness gate — **decision: inline in handler via injected `*atomic.Bool`** (no new middleware file). Rationale: RESEARCH §7 pattern is `searcher.indexReady.Load() → ErrSearchIndexNotReady → handler maps to 503`, and there are zero existing middlewares under `services/api/internal/middleware/` (no `services/api/internal/handler/middleware/` directory exists). Adding one would invent a precedent; inlining matches `services/api/internal/handler/search.go` RESEARCH §7 sketch exactly. | (no file) | — | RESEARCH §7 — `domain.ErrSearchIndexNotReady` errors.Is mapping in handler | role-match |
| 10 | `test/integration/search_test.go` (NEW) | test (integration) | read-write | `test/integration/authorization_test.go` lines 13–194 (two-user setupTestUser pattern) + `test/integration/conversation_test.go` lines 14–93 (CreateConversation request shape) | exact |
| 11 | `services/api/internal/service/search_test.go` (NEW; covers `Searcher.Search` orchestration + `ErrInvalidScope`) | test (unit) | read-write | `services/api/internal/service/titler_test.go` lines 16–80 (fakeRouter + nil-embedded fakeRepo + captureLogs) | exact |
| 12 | `services/api/internal/service/snippet_test.go` (NEW; covers `BuildSnippet` + `HighlightRanges`) | test (unit) | pure-render | same `titler_test.go` table-driven test pattern; RESEARCH §10 case examples | role-match (table-driven Go test) |
| 13 | `services/api/internal/repository/search_indexes_test.go` (NEW; idempotent `CreateMany` rerun) | test (unit) | read-only | RESEARCH §4 `EnsureSearchIndexes` + `pending_tool_call.go` `EnsurePendingToolCallsIndexes` lines 62–94 | role-match |
| 14 | `services/api/internal/handler/search_test.go` (NEW; covers 503-before-ready, 400-short-q, log-shape) | test (unit) | request-response | `services/api/internal/service/titler_test.go` `captureLogs` lines 64–71 (log-shape regression — Pitfall 8) | exact |
| 15 | `services/frontend/app/(app)/layout.tsx` (split into NavRail + ProjectPane + Cmd-K + PanelGroup) | page (client) | event-driven | same file lines 1–74 (existing `'use client'` boundary, useEffect lifecycle) | exact |
| 16 | `services/frontend/components/sidebar/NavRail.tsx` (NEW; extracted from `services/frontend/components/sidebar.tsx`) | component | pure-render | `services/frontend/components/sidebar.tsx` lines 95–183 (`SidebarContent` body — nav list + integration status + logout) | exact (extraction, not invention) |
| 17 | `services/frontend/components/sidebar/ProjectPane.tsx` (NEW; extracted from `sidebar.tsx`) | component | pure-render | `services/frontend/components/sidebar.tsx` lines 122–146 (the `isProjectsOrChatArea` subtree) | exact (extraction) |
| 18 | `services/frontend/components/sidebar/PinnedSection.tsx` (NEW) | component | pure-render | `services/frontend/components/sidebar/UnassignedBucket.tsx` lines 20–100 | exact |
| 19 | `services/frontend/components/sidebar/SidebarSearch.tsx` (NEW) | component | event-driven (debounced fetch) | `services/frontend/components/ui/popover.tsx` lines 1–33 (Radix Popover primitive); `services/frontend/components/chat/ChatHeader.tsx` lines 32–46 (narrow-selector React Query pattern); RESEARCH §8 (Cmd-K listener + CustomEvent) | role-match (no in-repo dropdown-search component) |
| 20 | `services/frontend/components/sidebar/SearchResultRow.tsx` (NEW) | component | pure-render | `services/frontend/components/sidebar/UnassignedBucket.tsx` lines 78–91 (chat-row Link); `services/frontend/components/chat/ProjectChip.tsx` lines 17–43 (project chip render) | role-match |
| 21 | `services/frontend/components/sidebar/ProjectSection.tsx` (extend with pin context-menu + indicator + roving-tabindex) | component | event-driven | same file lines 22–109; `services/frontend/components/chat/MoveChatMenuItem.tsx` lines 70–108 (Radix DropdownMenu Sub-menu pattern) | exact |
| 22 | `services/frontend/components/sidebar/UnassignedBucket.tsx` (extend with pin context-menu + indicator + roving-tabindex) | component | event-driven | same file lines 41–99; same DropdownMenu analog as #21 | exact |
| 23 | `services/frontend/components/chat/ChatHeader.tsx` (add bookmark pin button) | component | event-driven | same file lines 32–58 (memoized narrow-selector pattern — D-11 mitigation) | exact |
| 24 | `services/frontend/components/chat/ProjectChip.tsx` (add `size?: 'xs'\|'sm'\|'md'` prop) | component | pure-render | same file lines 14–43 (existing `chipBase` Tailwind class — extend with size variants) | exact |
| 25 | `services/frontend/hooks/useChat.ts` (extend with `?highlight=msgId` parser) | hook | event-driven | same file lines 244–281 (existing `handleSSEEvent` with React Query invalidation on `done`); RESEARCH §9 (`useHighlightMessage` hook implementation) | role-match (additive) |
| 26 | `services/frontend/hooks/useDebouncedValue.ts` (NEW — no in-repo precedent) | hook | event-driven | RESEARCH §13 (250 ms locked); planner implements ~15-line `useState`+`useEffect`+`setTimeout` hook (see Shared Patterns §Debounce below) | NO ANALOG — flagged for planner |
| 27 | `services/frontend/hooks/useRovingTabIndex.ts` (NEW — no in-repo precedent) | hook | event-driven | RESEARCH §13 / W3C ARIA Authoring Practices windowsplitter; planner implements ~30-line hook | NO ANALOG — flagged for planner |
| 28 | `services/frontend/components/sidebar/__tests__/sidebar-axe.test.tsx` (NEW) | test (a11y) | pure-render | `services/frontend/components/sidebar/__tests__/ProjectSection.test.tsx` lines 1–100 (vitest + RTL + QueryClient wrapper); RESEARCH §3 axe matcher excerpt | role-match (vitest-axe is new dep) |
| 29 | `services/frontend/components/sidebar/__tests__/{NavRail,ProjectPane,PinnedSection,SidebarSearch,SearchResultRow}.test.tsx` (NEW) | test (unit) | pure-render | same `ProjectSection.test.tsx` analog as #28 | exact |
| 30 | `services/frontend/components/ui/sheet.tsx` | component | (REUSE, no edit) | same file — existing Radix Dialog wrapper | reuse-only |
| 31 | `services/frontend/vitest.setup.ts` (extend with `@chialab/vitest-axe` matchers) | config | (test-only) | same file lines 1–58; RESEARCH §3 setup excerpt | exact |
| 32 | `services/frontend/package.json` (add `react-resizable-panels` + `@chialab/vitest-axe`) | config (deps) | n/a | same file lines 13–75 | exact |
| 33 | `services/api/go.mod` (add `github.com/kljensen/snowball` v0.10.0) | config (deps) | n/a | same file lines 7–26 | exact |

**Gap flags for planner:**
- `useDebouncedValue.ts` and `useRovingTabIndex.ts` have NO in-repo analog. Planner must author both from scratch using the W3C ARIA Authoring Practices roving-tabindex pattern and the canonical 250 ms debounce-by-setTimeout shape (see Shared Patterns §6 below).
- `services/api/internal/handler/middleware/` does not exist; `services/api/internal/middleware/` is the actual path. The "search readiness middleware" in the brief is folded into the handler itself per RESEARCH §7.

---

## Pattern Assignments

### 1. `pkg/domain/mongo_models.go` (domain, struct extension)

**Analog:** same file, `Conversation` struct lines 39–50.

**Existing struct** (mongo_models.go:39-50) — copy bson-tag conventions verbatim:
```go
type Conversation struct {
    ID            string     `json:"id" bson:"_id,omitempty"`
    UserID        string     `json:"userId" bson:"user_id"`
    BusinessID    string     `json:"businessId" bson:"business_id"`
    ProjectID     *string    `json:"projectId,omitempty" bson:"project_id"`
    Title         string     `json:"title" bson:"title"`
    TitleStatus   string     `json:"titleStatus" bson:"title_status"`
    Pinned        bool       `json:"pinned" bson:"pinned"`
    LastMessageAt *time.Time `json:"lastMessageAt,omitempty" bson:"last_message_at,omitempty"`
    CreatedAt     time.Time  `json:"createdAt" bson:"created_at"`
    UpdatedAt     time.Time  `json:"updatedAt" bson:"updated_at"`
}
```

**Phase 19 change** — replace `Pinned bool` with `PinnedAt *time.Time` (RESEARCH §12; D-02 single-source-of-truth):
```go
// Pinned bool removed — single source of truth is PinnedAt != nil (Phase 19 D-02).
PinnedAt *time.Time `json:"pinnedAt,omitempty" bson:"pinned_at,omitempty"`
```

**Gotchas to copy from analog:**
- Use `*time.Time` (pointer + omitempty) like `LastMessageAt`, NOT `time.Time` zero-value, so JSON `null` is unambiguous.
- `bson:",omitempty"` IS allowed here (unlike `ProjectID` which deliberately omits it — see comment lines 33–38) because pinned-at-null is the natural "not pinned" state and the backfill explicitly writes `pinned_at: null` for legacy docs (RESEARCH §12 Step 1).
- Keep the existing `Pinned bool` field present until the migration runs OR remove it together with the migration; planner picks per D-02.

---

### 2. `services/api/internal/repository/conversation.go` (repo, index + search + pin)

**Analog A — index extension:** same file, `EnsureConversationIndexes` lines 224–243.

**Existing index block** (conversation.go:224-243) — extend the `models` slice with two more `IndexModel` entries:
```go
func EnsureConversationIndexes(ctx context.Context, db *mongo.Database) error {
    coll := db.Collection("conversations")
    models := []mongo.IndexModel{
        {
            Keys: bson.D{
                {Key: "user_id", Value: 1},
                {Key: "business_id", Value: 1},
                {Key: "title_status", Value: 1},
            },
            Options: options.Index().SetName("conversations_user_biz_title_status"),
        },
        // Phase 19 — append two more here (RESEARCH §11):
        // 1. Compound index for sidebar list: user_id+business_id+project_id+pinned_at:-1+last_message_at:-1
        //    Name: "conversations_user_biz_proj_pinned_recency"
        // 2. Title text index: SetDefaultLanguage("russian") + SetWeights({title: 20}) + SetBackground(true)
        //    Name: "conversations_title_text_ru"
    }
    if _, err := coll.Indexes().CreateMany(ctx, models); err != nil {
        if mongo.IsDuplicateKeyError(err) {
            return nil
        }
        return fmt.Errorf("ensure conversation indexes: %w", err)
    }
    return nil
}
```

**Analog B — atomic conditional update for pin/unpin:** same file, `UpdateTitleIfPending` lines 155–177.
```go
func (r *conversationRepository) UpdateTitleIfPending(ctx context.Context, id, title string) error {
    filter := bson.M{
        "_id": id,
        "title_status": bson.M{
            "$in": []interface{}{domain.TitleStatusAutoPending, nil},
        },
    }
    update := bson.M{
        "$set": bson.M{
            "title":        title,
            "title_status": domain.TitleStatusAuto,
            "updated_at":   time.Now(),
        },
    }
    result, err := r.collection.UpdateOne(ctx, filter, update)
    if err != nil {
        return fmt.Errorf("update title if pending: %w", err)
    }
    if result.MatchedCount == 0 {
        return domain.ErrConversationNotFound
    }
    return nil
}
```

**Phase 19 Pin/Unpin shape** — same atomic conditional pattern, scope by `(user_id, business_id)`:
```go
func (r *conversationRepository) Pin(ctx context.Context, id, businessID, userID string) error {
    now := time.Now().UTC()
    filter := bson.M{"_id": id, "business_id": businessID, "user_id": userID}
    update := bson.M{"$set": bson.M{"pinned_at": now, "updated_at": now}}
    res, err := r.collection.UpdateOne(ctx, filter, update)
    if err != nil { return fmt.Errorf("pin conversation: %w", err) }
    if res.MatchedCount == 0 { return domain.ErrConversationNotFound }
    return nil
}
// Unpin: same shape, $set {pinned_at: nil, updated_at: now}.
```

**Analog C — search-titles `Find` shape:** RESEARCH §4 (lines 316–341 of RESEARCH.md). Phase-1 `Find` with `$text` + scoping equality + `SetProjection` of `$meta:textScore` + `SetSort` by score + `SetLimit`. Reuse `r.collection.Find` like `ListByUserID` (conversation.go:55-74) but with the `$text`-aware projection.

**Gotchas to copy:**
- `fmt.Errorf("...: %w", err)` wrap convention — every error path in this file (lines 36, 49, 65, 71, ...) follows this shape.
- Sentinel `domain.ErrConversationNotFound` returned on `MatchedCount == 0` — never on driver-level errors.
- Bson key style: `bson.D{{Key: ..., Value: ...}}` for ordered keys (indexes, sorts), `bson.M{...}` for unordered filters and `$set` payloads. Match the existing file's mix exactly.
- `SetBackground(true)` is a no-op on Mongo 4.2+ (RESEARCH §4) but keep it for SEARCH-06 source-readability.
- `business_id` and `user_id` in pin/unpin filter — defense-in-depth scope (D-12 / Pitfalls §19).

---

### 3. `services/api/internal/repository/message.go` (repo, aggregation pipeline)

**Analog:** same file, `FindByConversationActive` lines 95–115 (filter + sort + decode shape — but a `FindOne`, not aggregation; closest in this repo).

**Existing query shape** (message.go:95-115):
```go
func (r *messageRepository) FindByConversationActive(ctx context.Context, conversationID string) (*domain.Message, error) {
    filter := bson.M{
        "conversation_id": conversationID,
        "role":            "assistant",
        "status": bson.M{"$in": []string{
            domain.MessageStatusPendingApproval,
            domain.MessageStatusInProgress,
        }},
    }
    opts := options.FindOne().SetSort(bson.D{{Key: "created_at", Value: -1}})

    var msg domain.Message
    err := r.collection.FindOne(ctx, filter, opts).Decode(&msg)
    if err != nil {
        if errors.Is(err, mongo.ErrNoDocuments) {
            return nil, domain.ErrMessageNotFound
        }
        return nil, fmt.Errorf("find active message: %w", err)
    }
    return &msg, nil
}
```

**Phase 19 `SearchByConversationIDs` pipeline** — RESEARCH §5 lines 391–438 is the literal target. Six-stage `mongo.Pipeline` with `$match`-first-because-`$text`-rule, `$addFields` for score, per-message `$sort`, `$group` by `conversation_id`, group-level `$sort`, `$limit`. Aggregate cursor + `cur.All(ctx, &hits)` + `defer cur.Close(ctx)` (mirrors `ListByConversationID` lines 48–58).

**Gotchas:**
- `$text` MUST be in the FIRST `$match` stage (Mongo manual rule — RESEARCH §5).
- `Message` document has NO `business_id` field (verified `pkg/domain/mongo_models.go:52-70`); cross-tenant scope is enforced by `conversation_id ∈ allowlist`, where the allowlist comes from a Phase-1 conversations query that DOES filter `{user_id, business_id, project_id?}`.
- `$in: convIDs` cap at 1000 elements (RESEARCH §15 Q10). Hardcode `const MaxScopedConversations = 1000`; log warning + truncate by recency on overflow.
- Decode into `[]MessageSearchHit` — define this struct as a sibling of `domain.Message` either in this file or in `pkg/domain/` (planner picks; if it stays internal to `services/api/internal/repository`, no need to expose via `pkg/domain`).
- Error-wrap convention: `fmt.Errorf("search messages aggregate: %w", err)` and `fmt.Errorf("decode search hits: %w", err)`.

---

### 4. `services/api/internal/repository/mongo_backfill.go` (repo, idempotent migration)

**Analog:** same file, `BackfillConversationsV15` lines 35–100.

**Existing pattern (mongo_backfill.go:35-100)** — copy literally, change marker name + per-field $set blocks:
```go
const SchemaMigrationPhase15 = "phase-15-projects-foundation"

func BackfillConversationsV15(ctx context.Context, db *mongo.Database) error {
    conversations := db.Collection("conversations")
    marker := db.Collection("schema_migrations")

    // Fast-path: if marker exists, skip — idempotent on restart.
    var existing bson.M
    err := marker.FindOne(ctx, bson.M{"_id": SchemaMigrationPhase15}).Decode(&existing)
    if err == nil {
        slog.InfoContext(ctx, "phase 15 backfill already applied", "marker", SchemaMigrationPhase15)
        return nil
    }
    if !errors.Is(err, mongo.ErrNoDocuments) {
        return fmt.Errorf("read schema_migrations marker: %w", err)
    }

    // Per-field guarded $set so newer docs that already have the field are
    // untouched. Unrolled per field so the {$exists: false} guard is literal
    // and easy to audit.
    if err := backfillField(ctx, conversations, "project_id",
        bson.M{"project_id": bson.M{"$exists": false}},
        bson.M{"$set": bson.M{"project_id": nil}}); err != nil {
        return err
    }
    // ... more guarded backfillField calls ...

    // Marker (one-shot; upsert so restart after partial run does not fail).
    _, err = marker.UpdateOne(ctx,
        bson.M{"_id": SchemaMigrationPhase15},
        bson.M{"$set": bson.M{
            "_id":        SchemaMigrationPhase15,
            "applied_at": time.Now().UTC(),
        }},
        options.UpdateOne().SetUpsert(true),
    )
    // ...
}
```

**Phase 19 deltas** — RESEARCH §12 lines 1086–1140 spell out the V19 body. Three steps under the same marker-fast-path shell:
1. `$set pinned_at: null` where `{pinned_at: {$exists: false}}` — uses `backfillField` helper (mongo_backfill.go:104-112) verbatim.
2. `$set pinned_at: $updated_at` where `{pinned: true, pinned_at: nil}` — uses `mongo.Pipeline` (aggregation update) like the `last_message_at` block at lines 75–85.
3. `$unset pinned` where `{pinned: {$exists: true}}` — drops legacy bool; uses raw `UpdateMany` (no helper).

**Gotchas:**
- Marker constant name MUST be `SchemaMigrationPhase19` (mirrors V15 naming).
- Aggregation-pipeline updates are the only way to reference field values inside `$set` (`"$updated_at"` syntax, line 76–78). If you need just a static value, plain `bson.M{"$set": ...}` is enough and goes through `backfillField`.
- `slog.InfoContext` log-line shape `"phase 19 backfill ..." + "marker" key` — copy from lines 43, 98.

---

### 5. `services/api/internal/service/search.go` (service, NEW)

**Analog:** `services/api/internal/service/titler.go` lines 60–243.

**Constructor convention** (titler.go:80-91) — panic on nil deps, mandatory-deps philosophy:
```go
func NewTitler(router chatCaller, repo domain.ConversationRepository, model string) *Titler {
    if router == nil {
        panic("NewTitler: router cannot be nil")
    }
    if repo == nil {
        panic("NewTitler: repo cannot be nil")
    }
    if model == "" {
        panic("NewTitler: model cannot be empty (set TITLER_MODEL or LLM_MODEL)")
    }
    return &Titler{router: router, repo: repo, model: model}
}
```

**Note:** alternative shape used elsewhere is `(*X, error)` (handler/auth.go:40-49). Both exist in the codebase; `panic` is idiomatic for hard wiring bugs surfaced at startup, `error-return` is for handler factories that may fail under config. SearchService is wiring-only → use `panic` like Titler.

**Searcher type + readiness flag** (RESEARCH §7 lines 586–605):
```go
type Searcher struct {
    convRepo   domain.ConversationRepository
    msgRepo    domain.MessageRepository
    indexReady *atomic.Bool       // pointer so the *handler* shares the same flag
}

func (s *Searcher) MarkIndexesReady() { s.indexReady.Store(true) }
```

**Orchestration body** (RESEARCH §5 lines 453–476) — pull from titler.go:113-243 the slog-metadata-only logging, multi-step pipeline-with-early-return shape:
```go
func (s *Searcher) Search(ctx context.Context, businessID, userID, query string, projectID *string, limit int) ([]SearchResult, error) {
    if businessID == "" || userID == "" {
        return nil, domain.ErrInvalidScope     // §6 cross-tenant guard
    }
    if !s.indexReady.Load() {
        return nil, domain.ErrSearchIndexNotReady   // §7
    }

    titleHits, _, err := s.convRepo.SearchTitles(ctx, businessID, userID, query, projectID, limit)
    if err != nil { return nil, err }
    scopedIDs := s.convRepo.ScopedConversationIDs(ctx, businessID, userID, projectID)
    msgHits, err := s.msgRepo.SearchByConversationIDs(ctx, query, scopedIDs, limit*2)
    if err != nil { return nil, err }

    return mergeAndRank(titleHits, msgHits, /*titleW=*/ 20, /*contentW=*/ 10, limit), nil
}
```

**slog-metadata-only convention** (titler.go:136-143) — every search log line carries `query_length`, `user_id`, `business_id`; **never** `query` itself (SEARCH-07 — Pitfalls §15):
```go
slog.WarnContext(ctx, "auto-title: llm error",
    "conversation_id", conversationID,
    "business_id", businessID,
    "prompt_length", promptLen,        // <-- LENGTH, not body
    "rejected_by", "llm_error",
    "duration_ms", time.Since(metricStart).Milliseconds(),
    "error", err,
)
```

**Gotchas:**
- New sentinels: add `domain.ErrInvalidScope` and `domain.ErrSearchIndexNotReady` to `pkg/domain/errors.go` (mirrors `ErrConversationNotFound` lookup pattern; planner adds these in plan 19-03 task 1).
- `chatCaller` interface (titler.go:58-60) is the package-private mocking seam pattern — apply to repo deps too. Phase 19 service can either depend on the concrete `domain.ConversationRepository` interface (already exists) or define a private `searchRepo` interface for tests (planner picks; titler.go uses neither — repo is concrete domain interface).

---

### 6. `services/api/internal/service/snippet.go` (service helpers, NEW)

**Analog:** `services/api/internal/service/titler.go` lines 251–273 (`sanitizeTitle`, `untitledChatRussian`) — pure rune-aware string helpers.

**Existing shape (titler.go:251-261):**
```go
func sanitizeTitle(raw string) string {
    s := strings.TrimSpace(raw)
    s = strings.Trim(s, `"'«»“”`)
    s = strings.TrimRight(s, ".!?;:")
    s = strings.TrimSpace(s)
    if utf8.RuneCountInString(s) > titleMaxChars {
        runes := []rune(s)
        s = string(runes[:titleMaxChars])
    }
    return s
}
```

**Phase 19 `BuildSnippet` + `firstStemMatch` + `expandLeftToBoundary` + `expandRightToBoundary`** — RESEARCH §10 lines 870–963 is the literal target. Pure functions only; no DB, no slog. Test via table-driven `_test.go` in same package (RESEARCH §10 lines 967–988).

**Gotchas:**
- Russian runes are multi-byte. Use `[]rune(content)` for token boundaries (titler.go:257-259), then convert back to byte offsets for the JS frontend (`len(string(runes[:i]))` idiom — RESEARCH §1 lines 122–125).
- Snowball lib import: `github.com/kljensen/snowball/russian` (NOT `github.com/kljensen/snowball` directly — the language is a subpackage; RESEARCH §1 line 86).
- `russian.Stem(token, false)` — second arg `stemStopWords` is `false` to leave Russian stop-words alone (Mongo's stemmer already filters these); ensures highlight ranges don't mark stop-words.

---

### 7. `services/api/internal/handler/search.go` (handler, NEW)

**Analog:** `services/api/internal/handler/conversation.go` lines 202–284 (auth → parse query → call repo → writeJSON / writeJSONError).

**Existing handler shape (conversation.go:243-284):**
```go
func (h *ConversationHandler) GetConversation(w http.ResponseWriter, r *http.Request) {
    userID, err := middleware.GetUserID(r.Context())
    if err != nil {
        writeJSONError(w, http.StatusUnauthorized, "unauthorized")
        return
    }

    conversationID := chi.URLParam(r, "id")

    if len(conversationID) != 24 {
        writeJSONError(w, http.StatusBadRequest, "invalid conversation id")
        return
    }
    // ...

    conversation, err := h.conversationRepo.GetByID(r.Context(), conversationID)
    if err != nil {
        if errors.Is(err, domain.ErrConversationNotFound) {
            writeJSONError(w, http.StatusNotFound, "conversation not found")
            return
        }
        slog.Error("failed to get conversation", "error", err)
        writeJSONError(w, http.StatusInternalServerError, "internal server error")
        return
    }

    if conversation.UserID != userID.String() {
        writeJSONError(w, http.StatusForbidden, "forbidden")
        return
    }

    writeJSON(w, http.StatusOK, conversation)
}
```

**Phase 19 search handler shape** (RESEARCH §7 lines 635–676):
```go
func (h *SearchHandler) Search(w http.ResponseWriter, r *http.Request) {
    q := strings.TrimSpace(r.URL.Query().Get("q"))
    if len(q) < 2 {
        writeJSONError(w, http.StatusBadRequest, "query too short")
        return
    }
    userID, err := middleware.GetUserID(r.Context())
    if err != nil {
        writeJSONError(w, http.StatusUnauthorized, "unauthorized")
        return
    }
    biz, err := h.businessSvc.GetByUserID(r.Context(), userID)
    if err != nil { /* ... 401/500 ... */ }

    var projectID *string
    if p := r.URL.Query().Get("project_id"); p != "" { projectID = &p }
    limit := parseLimit(r.URL.Query().Get("limit"), 20, 50)

    results, err := h.searcher.Search(r.Context(), biz.ID.String(), userID.String(), q, projectID, limit)
    if errors.Is(err, domain.ErrSearchIndexNotReady) {
        w.Header().Set("Retry-After", "5")
        writeJSONError(w, http.StatusServiceUnavailable, "search index initializing")
        return
    }
    // ... ErrInvalidScope → 500, generic err → 500, success → writeJSON ...
}
```

**Constructor** — same pattern as `NewConversationHandler` (conversation.go:43-72): factory returns `(*SearchHandler, error)` and rejects nil deps with `fmt.Errorf("NewSearchHandler: ... cannot be nil")`.

**Gotchas:**
- Use `middleware.GetUserID(r.Context())` (NOT `middleware.UserIDFromContext` from RESEARCH §7's sketch — the actual helper name in this repo is `GetUserID`, verified via conversation.go:124, 204, 245, 293, 339, 387, 487).
- `writeJSON`, `writeJSONError`, `writeValidationError` are package-level helpers in `services/api/internal/handler/response.go` (referenced from conversation.go); reuse, don't reimplement.
- SEARCH-07 logging: every error log carries `user_id`, `business_id`, `query_length` — never `q` itself.

---

### 8. `services/api/cmd/main.go` (config / wiring, MODIFY)

**Analog:** same file, Phase 18 backfill+index block lines 88–130; HITL service+handler wiring lines 365–377.

**Existing index/backfill block (main.go:88-130):**
```go
// Phase 15 Mongo backfill — idempotent, marker-gated.
backfillCtx, backfillCancel := context.WithTimeout(ctx, 30*time.Second)
if err := repository.BackfillConversationsV15(backfillCtx, mongoDB); err != nil {
    backfillCancel()
    slog.ErrorContext(backfillCtx, "phase 15 backfill failed", "error", err)
    return fmt.Errorf("phase 15 backfill: %w", err)
}
backfillCancel()

// ... Phase 16 indexes ...

// Phase 18 Plan 03 (D-08a): compound index {user_id, business_id, title_status} on conversations.
indexesCtx2, indexesCancel2 := context.WithTimeout(ctx, 30*time.Second)
if err := repository.EnsureConversationIndexes(indexesCtx2, mongoDB); err != nil {
    indexesCancel2()
    slog.ErrorContext(indexesCtx2, "failed to ensure conversation indexes", "error", err)
    return fmt.Errorf("ensure conversation indexes: %w", err)
}
indexesCancel2()
```

**Phase 19 additions** (RESEARCH §7 lines 610–629):
1. After `EnsureConversationIndexes`, call `repository.EnsureSearchIndexes` with a 60s timeout (text-index build can exceed 30s on larger corpora).
2. After indexes, call `repository.BackfillConversationsV19` with the same 30s pattern.
3. Construct `searcher := service.NewSearcher(conversationRepo, messageRepo)`.
4. Call `searcher.MarkIndexesReady()` AFTER `EnsureSearchIndexes` returns nil.
5. Construct `searchHandler := handler.NewSearchHandler(searcher, businessService)`.
6. Add `Search *handler.SearchHandler` to the `Handlers` struct in `services/api/internal/router/router.go:19-34` and register `r.Get("/search", handlers.Search.Search)` inside the protected `r.Group` block (router.go:71-164).

**Gotchas:**
- Each `WithTimeout` MUST have a paired `cancel()`. Existing block calls `cancel()` on the error path AND just after the call; replicate exactly to avoid `lostcancel` lints.
- Construction order matters: `MarkIndexesReady()` MUST be called only after `EnsureSearchIndexes` returns nil, NOT before (RESEARCH §7).
- Add `searcher` to the `Handlers` registration block (main.go:385-399) and the `Search` chi-router route (router.go:111-164).

---

### 9. (No new middleware file)

The brief mentioned `services/api/internal/handler/middleware/search_ready.go`. This directory does not exist; the actual path for chi middlewares in this repo is `services/api/internal/middleware/`. RESEARCH §7 implements readiness as a `*atomic.Bool` injected into `Searcher`, which the handler reads via `errors.Is(err, domain.ErrSearchIndexNotReady)`. No middleware file needed; the gate is a sentinel-error mapping inside the handler.

---

### 10. `test/integration/search_test.go` (test, NEW)

**Analog A — two-user setup:** `test/integration/authorization_test.go` lines 13–47.
```go
func TestMultiUserAuthorization(t *testing.T) {
    cleanupDatabase(t)

    // Setup: create two users
    accessTokenA := setupTestUser(t, "userA@example.com", "password123")
    accessTokenB := setupTestUser(t, "userB@example.com", "password123")

    // User A creates business
    setupTestBusiness(t, accessTokenA)
    // User B creates business
    setupTestBusiness(t, accessTokenB)

    var conversationIDA string
    t.Run("UserACreatesConversation", func(t *testing.T) {
        // ... POST /api/v1/conversations with bearer accessTokenA ...
    })
    // ...
}
```

**Analog B — assertion shape:** authorization_test.go:72-94 (the cross-tenant negative test):
```go
t.Run("UserACannotAccessUserBConversation", func(t *testing.T) {
    req, _ := http.NewRequest("GET", baseURL+"/api/v1/conversations/"+conversationIDB, nil)
    req.Header.Set("Authorization", "Bearer "+accessTokenA)

    resp, err := httpClient.Do(req)
    require.NoError(t, err)
    defer resp.Body.Close()

    // Should return 403 Forbidden (authorization check in handler)
    assert.Equal(t, http.StatusForbidden, resp.StatusCode)
})
```

**Phase 19 test cases** (RESEARCH §6 lines 503–558):
- `TestSearchCrossTenant` — User A's `/search?q=инвойс` returns only User A's conversations.
- `EmptyQueryReturns400` — `q < 2 chars` → 400.
- `MissingBearerReturns401`.
- `TestSearch_503BeforeReady` — boot-time race; planner decides whether to expose the readiness flag for tests or skip this case (RESEARCH §13 lines 1224).
- `TestSearchAggregatedShape` — response is `[]SearchResultRow` keyed by conversation, not raw messages.
- `TestSearchProjectScope` — `?project_id=…` filters out other-project hits.

**Helpers to add** to whichever file holds `setupTestUser` / `setupTestBusiness` / `cleanupDatabase` (planner finds via `Grep` in `test/integration/main_test.go`):
- `createConversationWithMessage(t, token, title, msg) string` — RESEARCH §6 helper sketch.

**Gotchas:**
- `cleanupDatabase(t)` MUST be the first call (authorization_test.go:14) — wipes Mongo + Postgres between tests.
- `t.Setenv` over `os.Setenv` per AGENTS.md and project memory.
- Race detector: integration tests run with `-race` per RESEARCH §13.

---

### 11. `services/api/internal/service/search_test.go` (test, NEW)

**Analog:** `services/api/internal/service/titler_test.go` lines 16–80.

**Existing fake-router pattern (titler_test.go:20-36):**
```go
type fakeRouter struct {
    mu            sync.Mutex
    returnContent string
    returnErr     error
    lastReq       *llm.ChatRequest
}

func (f *fakeRouter) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
    f.mu.Lock()
    defer f.mu.Unlock()
    reqCopy := req
    f.lastReq = &reqCopy
    if f.returnErr != nil {
        return nil, f.returnErr
    }
    return &llm.ChatResponse{Content: f.returnContent}, nil
}
```

**Nil-embedded fake-repo trick (titler_test.go:46-58)** — for repos where most methods are forbidden (W-04 resolution; loud-failure):
```go
type fakeConvRepo struct {
    domain.ConversationRepository // nil — sentinel for "must not be called"
    mu                            sync.Mutex
    updateCalls                   []struct{ ID, Title string }
    updateRetErr                  error
}

func (r *fakeConvRepo) UpdateTitleIfPending(_ context.Context, id, title string) error {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.updateCalls = append(r.updateCalls, struct{ ID, Title string }{id, title})
    return r.updateRetErr
}
```

**Log-shape regression helper (titler_test.go:64-71):**
```go
func captureLogs(t *testing.T) *bytes.Buffer {
    t.Helper()
    buf := &bytes.Buffer{}
    prevLogger := slog.Default()
    slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
    t.Cleanup(func() { slog.SetDefault(prevLogger) })
    return buf
}
```

**Phase 19 test deltas:**
- Use `captureLogs(t)` to assert `bytes.Contains(buf.Bytes(), []byte("query_length"))` and `!bytes.Contains(buf.Bytes(), []byte(actualQueryText))` — SEARCH-07.
- Test `ErrInvalidScope` returned for empty `businessID` / `userID` (defense-in-depth).
- Test `ErrSearchIndexNotReady` returned when `searcher.indexReady.Load() == false`.

**Gotchas:**
- `t.Setenv` for any env-driven config (project memory). The Searcher itself takes no env, but if planner adds a `MaxScopedConversations` override, env-driven tests must use `t.Setenv`.

---

### 12. `services/api/internal/service/snippet_test.go` (test, NEW)

**Analog:** RESEARCH §10 lines 967–988 (table-driven cases). Generic Go table-driven shape; no exact in-repo analog beyond the convention used pervasively (e.g., titler_test.go cases).

**Test-table shape from RESEARCH §10:**
```go
{
    name:    "match in middle, both ellipses",
    content: "Доброе утро. Я хочу запланировать пост в Telegram на пятницу вечером, чтобы охватить аудиторию выходных.",
    stems:   map[string]struct{}{"запланирова": {}},
    want:    "…Я хочу запланировать пост в Telegram на пятницу вечером, чтобы охватить аудиторию…",
}
```

**Gotchas:** pure Go test, no Mongo. Run under `-race` like all other Go tests.

---

### 13. `services/api/internal/repository/search_indexes_test.go` (test, NEW)

**Analog:** `services/api/internal/repository/pending_tool_call.go` lines 62–94 (`EnsurePendingToolCallsIndexes` shape — RESEARCH §13 references this).

**Phase 19 test:** call `EnsureSearchIndexes` twice; second call must return nil (idempotency). Use a real test Mongo instance from `test/integration/main_test.go` setup, OR the in-package `miniredis`-style approach if Mongo has an embedded test driver (RESEARCH §13 leaves this to planner).

---

### 14. `services/api/internal/handler/search_test.go` (test, NEW)

**Analog:** `services/api/internal/service/titler_test.go` lines 64–71 (captureLogs) — same log-shape regression pattern applies to the handler.

**Phase 19 test deltas:**
- `TestSearchHandler_503BeforeReady` — readiness flag false → 503 with `Retry-After: 5`.
- `TestSearchHandler_400OnShortQuery` — `q=""` and `q="a"` → 400.
- `TestSearchHandler_LogShape` — captureLogs + assert metadata fields, no `q` body.

---

### 15. `services/frontend/app/(app)/layout.tsx` (page, MODIFY)

**Analog:** same file lines 1–74 (existing `'use client'` + auth-bootstrap pattern).

**Existing render shape (layout.tsx:68-73):**
```tsx
return (
    <div className="flex h-screen flex-col overflow-hidden md:flex-row">
        <Sidebar />
        <main className="flex-1 overflow-y-auto bg-gray-50">{children}</main>
    </div>
);
```

**Phase 19 deltas** (RESEARCH §2 lines 165–175 + §8 lines 686–719):
1. Replace `<Sidebar />` with `<NavRail />` always-rendered + `<PanelGroup>` containing route-conditional `<ProjectPane>` and `{children}`. Use `react-resizable-panels` with `autoSaveId="onevoice:sidebar-width"`:
   ```tsx
   import { Panel, PanelGroup, PanelResizeHandle } from 'react-resizable-panels';
   // ...
   <div className="flex h-screen ...">
     <NavRail />
     <PanelGroup direction="horizontal" autoSaveId="onevoice:sidebar-width">
       {showProjectPane && (
         <>
           <Panel defaultSize={20} minSize={15} maxSize={40}>
             <ProjectPane />
           </Panel>
           <PanelResizeHandle className="w-px bg-gray-700 hover:bg-gray-500" />
         </>
       )}
       <Panel><main>{children}</main></Panel>
     </PanelGroup>
   </div>
   ```
2. Add Cmd/Ctrl-K listener (RESEARCH §8 lines 700–719):
   ```tsx
   const SIDEBAR_FOCUS_EVENT = 'onevoice:sidebar-search-focus';
   useEffect(() => {
     function onKeydown(e: KeyboardEvent) {
       if ((e.metaKey || e.ctrlKey) && (e.key === 'k' || e.key === 'K')) {
         e.preventDefault();
         window.dispatchEvent(new CustomEvent(SIDEBAR_FOCUS_EVENT));
       }
     }
     window.addEventListener('keydown', onKeydown);
     return () => window.removeEventListener('keydown', onKeydown);
   }, []);
   ```
3. `showProjectPane = pathname.startsWith('/chat') || pathname.startsWith('/projects')` — same boolean as existing sidebar.tsx:71.

**Gotchas:**
- `useEffect` cleanup: layout.tsx:51-54 already shows the AbortController cleanup convention. Apply same care to the Cmd-K listener.
- `'use client'` directive at line 1 is already present — no change needed.
- The `react-resizable-panels` `autoSaveId` writes to `localStorage` automatically (RESEARCH §2 line 177); D-15 stable key satisfied.

---

### 16. `services/frontend/components/sidebar/NavRail.tsx` (component, NEW — extracted)

**Analog:** `services/frontend/components/sidebar.tsx` `SidebarContent` body lines 95–183 — extract the logo header + nav list + integration status + logout into a fixed-width vertical bar.

**Existing nav-list pattern (sidebar.tsx:103-150):**
```tsx
<nav className="flex-1 space-y-1 overflow-y-auto p-2">
    {navItems.map(({ href, label, icon: Icon }) => {
        const isActive = pathname.startsWith(href);
        return (
            <div key={href}>
                <Link href={href} onClick={onNavigate} className={cn(...)}>
                    <Icon size={18} />
                    {label}
                </Link>
                // ... project-subtree rendered here in current sidebar.tsx — REMOVE for NavRail; move to ProjectPane
            </div>
        );
    })}
</nav>
```

**Phase 19 NavRail shape:** strip the `{href === '/chat' && isProjectsOrChatArea && (...)}` subtree (lines 122–146) — that goes to `ProjectPane`. Keep the icon-only narrow rail (D-14: width 56–64 px, vertical icons only). Tooltips on hover via `<Tooltip>` from `services/frontend/components/ui/tooltip.tsx` (already in stack — package.json:31).

**Gotchas:** keep `'use client'` directive; uses `usePathname` + `useRouter`. Reuse `useAuthStore` for user email + logout.

---

### 17. `services/frontend/components/sidebar/ProjectPane.tsx` (component, NEW — extracted)

**Analog:** `services/frontend/components/sidebar.tsx` lines 122–146 (the `isProjectsOrChatArea` subtree).

**Existing tree (sidebar.tsx:122-146):**
```tsx
{href === '/chat' && isProjectsOrChatArea && (
    <div className="mt-1 space-y-1 border-l border-gray-700 pl-2">
        <UnassignedBucket
            conversations={unassigned}
            activeConversationId={activeConversationId}
            onNavigate={onNavigate}
        />
        {sortedProjects.map((p) => (
            <ProjectSection
                key={p.id}
                project={p}
                conversations={byProject[p.id] ?? []}
                activeConversationId={activeConversationId}
                onNavigate={onNavigate}
            />
        ))}
        <Link
            href="/projects/new"
            onClick={onNavigate}
            className="mt-1 block px-2 py-1 text-xs text-gray-500 hover:text-white"
        >
            + Новый проект
        </Link>
    </div>
)}
```

**Phase 19 deltas:**
1. Move this entire subtree to `ProjectPane.tsx`.
2. Add `<SidebarSearch />` at the top (D-06, D-11).
3. Insert `<PinnedSection conversations={pinned} ... />` between SidebarSearch and UnassignedBucket — visible only when `pinned.length > 0` (D-04).
4. Derive `pinned`, `unassigned`, `byProject` from `conversations`. `pinned = conversations.filter(c => c.pinnedAt != null).sort((a,b) => b.pinnedAt.localeCompare(a.pinnedAt))` — D-03.

**Gotchas:**
- Top-level `useMemo` for `pinned` to avoid recomputing on unrelated cache mutation — same flicker mitigation pattern as ChatHeader (D-11). Memoize with the conversations array reference key.

---

### 18. `services/frontend/components/sidebar/PinnedSection.tsx` (component, NEW)

**Analog:** `services/frontend/components/sidebar/UnassignedBucket.tsx` lines 20–100.

**Existing UnassignedBucket shape (UnassignedBucket.tsx:20-99):**
```tsx
export function UnassignedBucket({ conversations, activeConversationId, onNavigate }: Props) {
    const [collapsed, setCollapsed] = useState(false);
    // ...
    return (
        <div className="group/bucket">
            <div className="flex items-center gap-1 rounded-md px-2 py-1.5 text-sm text-gray-300 hover:bg-gray-800">
                <button onClick={() => setCollapsed((v) => !v)} aria-expanded={!collapsed}>
                    {collapsed ? <ChevronRight size={12} /> : <ChevronDown size={12} />}
                    <FolderMinus size={12} />
                    <span className="flex-1 truncate italic text-gray-400">Без проекта</span>
                    <span className="text-xs text-gray-500">· {count}</span>
                </button>
                {/* + button */}
            </div>
            {!collapsed && (
                <div className="ml-5 mt-0.5 space-y-0.5">
                    {visible.map((conv) => (
                        <Link key={conv.id} href={`/chat/${conv.id}`} onClick={onNavigate}
                              className={cn('block truncate rounded-md px-2 py-1 text-xs', conv.id === activeConversationId ? 'bg-gray-700 text-white' : 'text-gray-400 hover:bg-gray-800')}>
                            {conv.title}
                        </Link>
                    ))}
                </div>
            )}
        </div>
    );
}
```

**Phase 19 PinnedSection deltas:**
1. Header label `«Закреплённые»` (Russian, D-04).
2. Use `Bookmark` icon from `lucide-react` (already in package.json:38).
3. Each row also renders `<ProjectChip size="xs" projectId={conv.projectId} projectName={...}/>` to the right of the title (D-05).
4. NO `+ button` (no "create new pinned chat" — pin is a state on existing chats).
5. Hidden entirely when `conversations.length === 0` (D-04 — `if (conversations.length === 0) return null`).

**Gotchas:** sort `conversations` by `pinnedAt desc` (caller's responsibility, D-03 — `ProjectPane` does the sort).

---

### 19. `services/frontend/components/sidebar/SidebarSearch.tsx` (component, NEW)

**Analog A — Radix Popover wrapper:** `services/frontend/components/ui/popover.tsx` lines 1–33 (already in package).
```tsx
import * as PopoverPrimitive from '@radix-ui/react-popover';
const Popover = PopoverPrimitive.Root;
const PopoverTrigger = PopoverPrimitive.Trigger;
const PopoverContent = React.forwardRef<...>(({ ... }) => (
    <PopoverPrimitive.Portal>
        <PopoverPrimitive.Content ... />
    </PopoverPrimitive.Portal>
));
```

**Analog B — narrow React Query selector for stable identity (avoid flicker):** `services/frontend/components/chat/ChatHeader.tsx` lines 32–46.
```tsx
function useConversationTitle(conversationId: string): string {
    const { data } = useQuery<Conversation[], Error, string>({
        queryKey: ['conversations'],
        queryFn: () => api.get('/conversations').then((r) => r.data),
        select: (list) => {
            const conv = list.find((c) => c.id === conversationId);
            // ...
        },
        enabled: !!conversationId,
    });
    return data ?? '';
}
```

**Phase 19 SidebarSearch shape:**
1. `<Popover open={isOpen} onOpenChange={setIsOpen}>` containing `<PopoverTrigger asChild><input ref={inputRef} ... /></PopoverTrigger>` and `<PopoverContent>` listing results.
2. Debounced input via `useDebouncedValue(query, 250)` (new hook — see #26).
3. React Query: `useQuery({ queryKey: ['search', businessId, projectId, debouncedQuery], queryFn: () => api.get('/search', {params: {q: debouncedQuery, project_id: projectId, limit: 20}}).then(r => r.data), enabled: debouncedQuery.length >= 2 })`.
4. Cmd-K consumer (RESEARCH §8 lines 727–733):
   ```tsx
   useEffect(() => {
     const input = inputRef.current;
     if (!input) return;
     function onFocus() { input.focus(); input.select(); }
     window.addEventListener('onevoice:sidebar-search-focus', onFocus);
     return () => window.removeEventListener('onevoice:sidebar-search-focus', onFocus);
   }, []);
   ```
5. Esc handler (RESEARCH §8 lines 739–747):
   ```tsx
   function onKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
     if (e.key === 'Escape') {
       setQuery('');
       setIsOpen(false);
       inputRef.current?.blur();
     }
   }
   ```
6. Mac-vs-non-Mac placeholder (RESEARCH §8 line 757):
   ```tsx
   const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform);
   const placeholder = isMac ? 'Поиск... ⌘K' : 'Поиск... Ctrl-K';
   ```

**Gotchas:**
- React Query key `['search', businessId, projectId, query]` — invalidate on businessId or projectId change to drop cache.
- The `current project` checkbox («По всему бизнесу») is local UI state inside this component and resets on `usePathname` change (RESEARCH §15 Q3).
- Empty result row: `«Ничего не найдено по «{query}»»` (D-13, locked Russian copy).

---

### 20. `services/frontend/components/sidebar/SearchResultRow.tsx` (component, NEW)

**Analog A — chat-row Link shape:** `services/frontend/components/sidebar/UnassignedBucket.tsx` lines 78–91.
```tsx
<Link key={conv.id} href={`/chat/${conv.id}`} onClick={onNavigate}
      className={cn('block truncate rounded-md px-2 py-1 text-xs transition-colors',
          conv.id === activeConversationId ? 'bg-gray-700 text-white' : 'text-gray-400 hover:bg-gray-800 hover:text-white')}>
    {conv.title}
</Link>
```

**Analog B — ProjectChip render:** `services/frontend/components/chat/ProjectChip.tsx` lines 17–43 (reuse with new `size="xs"` prop — see #24).

**Phase 19 SearchResultRow shape:**
- `<Link href={`/chat/${result.conversationId}?highlight=${result.topMessageId}`}>`.
- Inside: title row + ProjectChip (right) + snippet with `<mark>` ranges + date + `+N совпадений` badge (D-07, when `match_count > 1`).
- `<mark>` rendering: split snippet at the `[start, end]` byte ranges from D-09's `marks` array; wrap each match in `<mark className="bg-yellow-200/40 text-inherit">`.

**Gotchas:**
- Mark ranges are byte offsets (Go side `len(string(runes[:i]))`); JS `.slice()` is by UTF-16 code units. Russian Cyrillic is in BMP (1 code unit per rune), so byte-offsets-from-Go decoded as UTF-8 string `.slice()` works for Russian text but NOT for emoji or astral-plane characters. v1.3 user content is Russian-only → safe; document as v1.4 followup.
- Date format: `date-fns` (already in package.json:37) `format(parseISO(date), 'd MMM', {locale: ru})`.

---

### 21. `services/frontend/components/sidebar/ProjectSection.tsx` (component, MODIFY — extend)

**Analog:** same file lines 22–109 (existing render). Add context menu using the Radix DropdownMenu pattern from `services/frontend/components/chat/MoveChatMenuItem.tsx` lines 70–108.

**Existing context-menu pattern (MoveChatMenuItem.tsx:70-108):**
```tsx
<DropdownMenuSub>
    <DropdownMenuSubTrigger>Переместить в…</DropdownMenuSubTrigger>
    <DropdownMenuPortal>
        <DropdownMenuSubContent>
            {/* DropdownMenuItem entries */}
        </DropdownMenuSubContent>
    </DropdownMenuPortal>
</DropdownMenuSub>
```

**Phase 19 deltas:**
1. Wrap each chat-row `Link` (lines 86–100) in `<DropdownMenu>` with `<DropdownMenuTrigger asChild>` — the Link becomes a context-menu trigger via right-click (Phase 15 D-11 — already established pattern).
2. Add `<DropdownMenuItem>` `«Закрепить»` (or `«Открепить»` if `conv.pinnedAt != null`) above the existing `«Переименовать»`/`«Удалить»`/`«Move to ▸»` items.
3. Render a small `<Bookmark size={10} className="text-yellow-400" />` icon next to the title when `conv.pinnedAt != null` (D-01 — pinned indicator).
4. Apply `useRovingTabIndex` (new hook — see #27) at the conversation-list `<div className="ml-5 mt-0.5 space-y-0.5">` element (line 82) — Tab enters the list once, ↑/↓ move within (D-17).

**Gotchas:**
- Pin mutation must invalidate `['conversations']` React Query (Shared Patterns §1). The mutation lives in a new `usePinConversation` hook in `services/frontend/hooks/useConversations.ts`.
- The `«Открепить»` menu-item label depends on `conv.pinnedAt` — pure derived from props.

---

### 22. `services/frontend/components/sidebar/UnassignedBucket.tsx` (component, MODIFY — extend)

**Analog:** same file lines 41–99 + same DropdownMenu pattern as #21.

**Phase 19 deltas:** identical extensions to ProjectSection #21 (pin context-menu + indicator + roving-tabindex). Same pin mutation, same React Query invalidation.

---

### 23. `services/frontend/components/chat/ChatHeader.tsx` (component, MODIFY — add bookmark button)

**Analog:** same file lines 32–58 (existing memoized narrow-selector mitigation — D-01 explicitly references this).

**Existing pattern (ChatHeader.tsx:32-46):**
```tsx
function useConversationTitle(conversationId: string): string {
    const { data } = useQuery<Conversation[], Error, string>({
        queryKey: ['conversations'],
        queryFn: () => api.get('/conversations').then((r) => r.data),
        select: (list) => {
            const conv = list.find((c) => c.id === conversationId);
            if (!conv) return '';
            return conv.title === '' || conv.titleStatus === 'auto_pending' ? 'Новый диалог' : conv.title;
        },
        enabled: !!conversationId,
    });
    return data ?? '';
}
```

**Phase 19 — add a parallel narrow selector for `pinned` only:**
```tsx
function useConversationPinned(conversationId: string): boolean {
    const { data } = useQuery<Conversation[], Error, boolean>({
        queryKey: ['conversations'],
        queryFn: () => api.get('/conversations').then((r) => r.data),
        select: (list) => list.find((c) => c.id === conversationId)?.pinnedAt != null,
        enabled: !!conversationId,
    });
    return data ?? false;
}
```

**Bookmark button shape (placed in `rightSlot` of `ChatHeaderImpl`):**
- `<button>` with `<Bookmark className={pinned ? "fill-yellow-400 text-yellow-400" : "text-gray-400"} />` icon.
- `aria-label={pinned ? "Открепить чат" : "Закрепить чат"}`.
- onClick → `pinMutation.mutate({id: conversationId})` from same `usePinConversation` hook used in sidebar context menu.

**Gotchas:**
- D-01 USER OVERRIDE: subscribe to `pinned` ONLY, not to the entire conversation row. The `select` projection returns a primitive boolean; React Query short-circuits re-renders when boolean is unchanged. Same flicker mitigation as title.
- `memo(ChatHeaderImpl)` wrap stays at the bottom (line 58) — already memoized.

---

### 24. `services/frontend/components/chat/ProjectChip.tsx` (component, MODIFY — add `size` prop)

**Analog:** same file lines 14–43.

**Existing Tailwind class (ProjectChip.tsx:14-15):**
```tsx
const chipBase =
    'inline-flex items-center gap-1.5 rounded-md border px-2 py-0.5 text-xs transition-colors';
```

**Phase 19 deltas:**
```tsx
type Size = 'xs' | 'sm' | 'md';
const sizeClasses: Record<Size, string> = {
    xs: 'px-1 py-0 text-[10px] gap-1',
    sm: 'px-2 py-0.5 text-xs gap-1.5',  // current default
    md: 'px-3 py-1 text-sm gap-2',
};

interface Props {
    projectId: string | null;
    projectName?: string;
    size?: Size;
}

export function ProjectChip({ projectId, projectName, size = 'sm' }: Props) {
    // ... existing JSX, replace `chipBase` with `cn(chipBase, sizeClasses[size])`
}
```

**Gotchas:** existing call sites (sidebar.tsx, ChatHeader, etc.) pass no `size` → defaults to `'sm'` → no visual change. Only `<PinnedSection>` and `<SearchResultRow>` pass `size="xs"` (D-05).

---

### 25. `services/frontend/hooks/useChat.ts` (hook, MODIFY — add `?highlight=msgId`)

**Analog:** same file lines 244–281 (existing `handleSSEEvent` with React Query invalidation on `done` — Phase 18 D-10).

**Existing pattern (useChat.ts:244-281):**
```tsx
const handleSSEEvent = useCallback(
    (event: Record<string, unknown>) => {
        if (event.type === 'done') {
            queryClient.invalidateQueries({ queryKey: ['conversations'] });
        }
        // ...
    },
    [queryClient]
);
```

**Phase 19 deltas:** the additive change is a NEW hook `useHighlightMessage` (RESEARCH §9 lines 783–833) that lives in `services/frontend/hooks/useHighlightMessage.ts` and is invoked from `services/frontend/app/(app)/chat/[id]/page.tsx` (RESEARCH §9 Step 3). Per the brief, it is grouped with `useChat` here for proximity, but it's a separate file. The `useChat.ts` itself does NOT need modification — the `data-message-id` attribute is added in `MessageList`/`MessageBubble` (RESEARCH §9 Step 1) which is rendered by `useChat`'s consumer, not by `useChat` itself.

**Files actually touched for D-08:**
1. `services/frontend/hooks/useHighlightMessage.ts` (NEW) — RESEARCH §9 lines 783–833 verbatim.
2. `services/frontend/components/chat/MessageList.tsx` or `MessageBubble.tsx` — add `data-message-id={message.id}` attribute on the per-message wrapper element (RESEARCH §9 Step 1).
3. `services/frontend/app/(app)/chat/[id]/page.tsx` — `useHighlightMessage(!isLoading && messages.length > 0)` after `const { messages, isLoading } = useChat(...)`.
4. `services/frontend/app/globals.css` (or equivalent) — `[data-highlight='true']` keyframe (RESEARCH §9 Step 4).

**Gotchas:**
- `useSearchParams()` + `usePathname()` + `router.replace(pathname, {scroll: false})` is canonical Next.js 14 App Router (RESEARCH §9).
- `CSS.escape(target)` to guard against odd ObjectID characters in the selector.
- `prefers-reduced-motion` branch: animation off, static background-color.

---

### 26. `services/frontend/hooks/useDebouncedValue.ts` (hook, NEW)

**Analog:** NO IN-REPO ANALOG — flagged for planner. Use this canonical 14-line shape:
```ts
import { useEffect, useState } from 'react';

export function useDebouncedValue<T>(value: T, delayMs: number): T {
    const [debounced, setDebounced] = useState(value);
    useEffect(() => {
        const timer = setTimeout(() => setDebounced(value), delayMs);
        return () => clearTimeout(timer);
    }, [value, delayMs]);
    return debounced;
}
```

**Gotchas:**
- The brief locks 250 ms (D-13). Caller passes `useDebouncedValue(query, 250)`.
- Test: vitest fake timers (`vi.useFakeTimers()` + `vi.advanceTimersByTime(250)`). No in-repo precedent for fake-timers in this codebase, but the convention is standard vitest.

---

### 27. `services/frontend/hooks/useRovingTabIndex.ts` (hook, NEW)

**Analog:** NO IN-REPO ANALOG — flagged for planner. Implement per W3C ARIA Authoring Practices windowsplitter / listbox pattern (cited at the bottom of RESEARCH §13 sources).

Sketch (~30 LOC; exact implementation is planner's call):
```ts
import { useRef, useEffect } from 'react';

export function useRovingTabIndex(itemCount: number) {
    const containerRef = useRef<HTMLElement | null>(null);
    const focusedIdx = useRef(0);

    function focusItem(idx: number) {
        const items = containerRef.current?.querySelectorAll<HTMLElement>('[data-roving-item]');
        if (!items) return;
        items.forEach((el, i) => el.setAttribute('tabindex', i === idx ? '0' : '-1'));
        items[idx]?.focus();
        focusedIdx.current = idx;
    }

    function onKeyDown(e: React.KeyboardEvent<HTMLElement>) {
        if (e.key === 'ArrowDown') { e.preventDefault(); focusItem((focusedIdx.current + 1) % itemCount); }
        if (e.key === 'ArrowUp')   { e.preventDefault(); focusItem((focusedIdx.current - 1 + itemCount) % itemCount); }
        if (e.key === 'Home')      { e.preventDefault(); focusItem(0); }
        if (e.key === 'End')       { e.preventDefault(); focusItem(itemCount - 1); }
    }

    return { containerRef, onKeyDown };
}
```

Each `Link`/`button` inside the container gets `data-roving-item` and an initial `tabindex={i === 0 ? 0 : -1}`.

---

### 28. `services/frontend/components/sidebar/__tests__/sidebar-axe.test.tsx` (test, NEW)

**Analog A — vitest + RTL + QueryClient wrapper:** `services/frontend/components/sidebar/__tests__/ProjectSection.test.tsx` lines 1–53.
```tsx
import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { ProjectSection } from '../ProjectSection';

// Mock next/navigation
const pushMock = vi.fn();
vi.mock('next/navigation', () => ({
    useRouter: () => ({ push: pushMock, back: vi.fn(), replace: vi.fn() }),
}));

vi.mock('sonner', () => ({
    toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock('@/lib/api', () => ({
    api: {
        get: vi.fn(() => Promise.resolve({ data: [] })),
        // ...
    },
}));

function makeClient() {
    return new QueryClient({
        defaultOptions: { queries: { retry: false } },
    });
}

function Wrapper({ children }: { children: ReactNode }) {
    const client = makeClient();
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}
```

**Analog B — vitest-axe matcher pattern:** RESEARCH §3 lines 230–246.
```tsx
import { axe } from '@chialab/vitest-axe';
// ...
it('sidebar has no critical/serious a11y violations', async () => {
    const { container } = render(<Sidebar />, { wrapper: Wrapper });
    const results = await axe(container, { resultTypes: ['violations'] });
    const blocking = results.violations.filter(
        (v) => v.impact === 'critical' || v.impact === 'serious'
    );
    expect(blocking).toEqual([]);
});
```

**Gotchas:**
- `@chialab/vitest-axe` matchers must be registered in `vitest.setup.ts` (see #31).
- Filter by impact manually — `vitest-axe` matcher does not have built-in impact filter.

---

### 29. Per-component unit tests (`NavRail.test.tsx`, `ProjectPane.test.tsx`, `PinnedSection.test.tsx`, `SidebarSearch.test.tsx`, `SearchResultRow.test.tsx`)

**Analog:** `services/frontend/components/sidebar/__tests__/ProjectSection.test.tsx` lines 1–100.

Each test:
1. Imports the component under test.
2. Mocks `next/navigation`, `sonner`, `@/lib/api` per ProjectSection.test.tsx:11-30.
3. Wraps in `<QueryClientProvider>` (the `Wrapper` helper).
4. Renders, asserts via `screen.getByText` / `screen.getByRole`.
5. Uses `userEvent` for interactions (already in package.json:60).

**SidebarSearch-specific test cases:**
- 250 ms debounce: `vi.useFakeTimers()`, `userEvent.type(input, 'тест')`, `vi.advanceTimersByTime(249)` → no fetch fired; `vi.advanceTimersByTime(1)` → fetch fired.
- Cmd-K listener: dispatch `new KeyboardEvent('keydown', {metaKey: true, key: 'k'})` → input is focused.
- Esc: `userEvent.keyboard('{Escape}')` → input cleared, popover closed.

---

### 30. `services/frontend/components/ui/sheet.tsx` (REUSE — no edit)

**Analog:** itself. D-16 mandates reuse without modification. Mobile drawer auto-close hook lives in the consumer (`Sidebar` / `ProjectPane`), not in `<Sheet>` itself.

---

### 31. `services/frontend/vitest.setup.ts` (config, MODIFY)

**Analog:** same file lines 1–58.

**Existing setup (vitest.setup.ts:1):**
```ts
import '@testing-library/jest-dom';
// ... localStorage polyfill, ResizeObserver stub, Element prototype stubs ...
```

**Phase 19 addition** (RESEARCH §3 lines 220–225):
```ts
import * as matchers from '@chialab/vitest-axe/matchers';
import { expect } from 'vitest';
expect.extend(matchers);
```

**Gotchas:** append at the END of the file. Do not relocate existing polyfills; some Radix primitives still need ResizeObserver / hasPointerCapture even with the axe matcher loaded.

---

### 32. `services/frontend/package.json` (config, MODIFY)

**Analog:** same file.

**Phase 19 deltas (Wave 0):**
- `dependencies`: add `"react-resizable-panels": "^4"` (RESEARCH §2 line 158).
- `devDependencies`: add `"@chialab/vitest-axe": "^0.19.1"` (RESEARCH §3 line 215).

Run `pnpm install` and commit `pnpm-lock.yaml`.

---

### 33. `services/api/go.mod` (config, MODIFY)

**Analog:** same file.

**Phase 19 delta (Wave 0):**
```bash
cd services/api
go get github.com/kljensen/snowball@v0.10.0
go mod tidy
```

This adds the require line for `github.com/kljensen/snowball v0.10.0` (RESEARCH §1 lines 76–80). Verify license / pure-Go status / version on pkg.go.dev as final pre-merge sanity check.

---

## Shared Patterns

### 1. Atomic Mongo conditional update (write path)

**Source:** `services/api/internal/repository/conversation.go` lines 155–177 (`UpdateTitleIfPending`).

**Apply to:**
- Phase 19 `Pin` / `Unpin` (file #2 above).

**Convention:**
```go
filter := bson.M{
    "_id": id,
    "user_id": userID,           // Defense-in-depth scope (Pitfalls §19)
    "business_id": businessID,
    // optional state guard: "field": bson.M{"$in": [...]}
}
update := bson.M{"$set": bson.M{
    "field": newValue,
    "updated_at": time.Now(),
}}
result, err := r.collection.UpdateOne(ctx, filter, update)
if err != nil {
    return fmt.Errorf("descriptive verb: %w", err)
}
if result.MatchedCount == 0 {
    return domain.ErrConversationNotFound
}
return nil
```

### 2. Idempotent Mongo index creation

**Source:** `services/api/internal/repository/pending_tool_call.go` lines 62–94, `services/api/internal/repository/conversation.go` lines 224–243.

**Apply to:**
- Phase 19 `EnsureSearchIndexes` (file #2 above) and the extended `EnsureConversationIndexes`.

**Convention:**
```go
func EnsureXxxIndexes(ctx context.Context, db *mongo.Database) error {
    coll := db.Collection("xxx")
    models := []mongo.IndexModel{
        {Keys: bson.D{...}, Options: options.Index().SetName("xxx_name").SetXxx(...)},
    }
    if _, err := coll.Indexes().CreateMany(ctx, models); err != nil {
        if mongo.IsDuplicateKeyError(err) {
            return nil
        }
        return fmt.Errorf("ensure xxx indexes: %w", err)
    }
    return nil
}
```

### 3. Idempotent Mongo backfill with marker

**Source:** `services/api/internal/repository/mongo_backfill.go` lines 35–112.

**Apply to:** Phase 19 `BackfillConversationsV19` (file #4 above).

**Convention:** marker `_id` const → marker FindOne fast-path → guarded `{$exists: false}` per-field `$set` (use the existing `backfillField` helper) → marker `UpdateOne(SetUpsert(true))` at the end.

### 4. slog metadata-only logging (SEARCH-07 / Pitfalls §15 / D-16)

**Source:** `services/api/internal/service/titler.go` lines 136–143.

**Apply to:** all Phase 19 search-related log lines (handler, service, repo).

**Convention:** structured fields `("user_id", uid, "business_id", bid, "query_length", len(q), "duration_ms", time.Since(start).Milliseconds())`. **Never** `"query", q` or `"snippet", text` or any user-content body.

### 5. Defense-in-depth scope filter

**Source:** RESEARCH §6 cross-tenant pattern + `domain.ErrInvalidScope` sentinel.

**Apply to:** every new repo signature in Phase 19 (`SearchTitles`, `SearchByConversationIDs` via the `convIDs` allowlist, `Pin`, `Unpin`).

**Convention:** repo signature MUST include `businessID, userID string` for any conversation-scoped operation; empty values return `ErrInvalidScope` immediately.

### 6. Constructor convention (panic on nil deps for wiring-only types)

**Source:** `services/api/internal/service/titler.go` lines 80–91.

**Apply to:** `service.NewSearcher` (file #5).

**Alternative — `(*X, error)` for handlers:** `services/api/internal/handler/auth.go` lines 40–49 (also seen in `NewConversationHandler` lines 43–72). Use `error`-return for handler factories that may legitimately fail under config; use `panic` for service-level wiring bugs.

### 7. React Query cache invalidation on mutation

**Source:** `services/frontend/hooks/useConversations.ts` lines 16–24.

**Apply to:** new `usePinConversation` mutation (used by ProjectSection, UnassignedBucket, ChatHeader, PinnedSection).

**Convention:**
```ts
return useMutation<Conversation, Error, {id: string}>({
    mutationFn: ({id}) => api.post(`/conversations/${id}/pin`).then(r => r.data),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: ['conversations'] }); },
});
```

### 8. Narrow React Query selector for flicker mitigation

**Source:** `services/frontend/components/chat/ChatHeader.tsx` lines 32–46 (D-11).

**Apply to:** `ChatHeader` pin button (file #23). Subscribe to a primitive (`boolean`) projection of the conversation row, not the whole row.

### 9. Russian UI copy (locked)

**Source:** CONTEXT.md `<specifics>` block.

**Apply to:** every user-visible string in #15–#22.

**Locked strings:**
- `«Закреплённые»` (PinnedSection header)
- `«Без проекта»` (already exists)
- `«Поиск... ⌘K»` / `«Поиск... Ctrl-K»` (UA-detected)
- `«Ничего не найдено по «{q}»»` (empty result)
- `«По всему бизнесу»` (scope checkbox)
- `«+N совпадений»` (match-count badge)
- `«Закрепить»` / `«Открепить»` (context-menu)
- `«Новый проект»`, `«Новый диалог»` (already exist)

### 10. Test conventions

- **Go:** table-driven, `_test.go` in same package, `-race`, `t.Setenv` not `os.Setenv`. Source: titler_test.go.
- **Frontend:** vitest + `@testing-library/react` + `QueryClient` wrapper, mock `next/navigation` + `sonner` + `@/lib/api`. Source: ProjectSection.test.tsx:1-53.
- **Integration:** `cleanupDatabase(t)` first, `setupTestUser` + `setupTestBusiness` per user, bearer-token-scoped requests. Source: authorization_test.go:13-47.

---

## No Analog Found

| File | Role | Data Flow | Reason / Mitigation |
|------|------|-----------|---------------------|
| `services/frontend/hooks/useDebouncedValue.ts` | hook | event-driven | No existing debounce hook in repo. Planner authors a 14-line `useState`+`useEffect`+`setTimeout` shape (Shared §6 of this doc). Test via vitest fake timers. |
| `services/frontend/hooks/useRovingTabIndex.ts` | hook | event-driven | No existing roving-tabindex pattern in repo. Planner authors ~30 LOC per W3C ARIA Authoring Practices listbox/windowsplitter pattern. |
| `services/api/internal/handler/middleware/search_ready.go` | (not created) | — | Brief mentions this file but no `services/api/internal/handler/middleware/` directory exists. Pattern is folded into the search handler via `errors.Is(err, domain.ErrSearchIndexNotReady)` per RESEARCH §7 — no separate middleware file needed. |

---

## Metadata

**Analog search scope:**
- `services/api/internal/handler/`, `services/api/internal/service/`, `services/api/internal/repository/`, `services/api/internal/middleware/`, `services/api/cmd/`
- `services/api/migrations/`, `pkg/domain/`
- `services/frontend/components/sidebar/`, `services/frontend/components/chat/`, `services/frontend/components/ui/`, `services/frontend/hooks/`, `services/frontend/app/(app)/`
- `test/integration/`
- Config: `services/api/go.mod`, `services/frontend/package.json`, `services/frontend/vitest.setup.ts`

**Files scanned:** ~40 (per-file targeted reads of analogs cited in RESEARCH §1–§15 + the four `<canonical_refs>` "Existing code touchpoints").

**Pattern extraction date:** 2026-04-27.

**Key project conventions confirmed via AGENTS.md / CLAUDE.md system reminders during this run:**
- `pkg/AGENTS.md` — only shared code in `pkg/`; `domain/mongo_models.go` is the right place for `PinnedAt`. Snowball wraps stay inside `services/api/`.
- `services/api/AGENTS.md` — handler→service→repository layering; index creation at startup is established convention; **no Postgres migration needed** for Phase 19 (pure Mongo).
- `services/frontend/AGENTS.md` — server-by-default with `'use client'` only when hooks/events; Tailwind only; React Query for server state; Zustand for global UI; `function` declarations not arrow; `import type` for type-only imports; vitest+RTL for tests.

## PATTERN MAPPING COMPLETE
