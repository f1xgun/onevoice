---
phase: 19-search-sidebar-redesign
plan: 03
type: execute
wave: 2
depends_on: ["19-02"]
files_modified:
  - services/api/go.mod
  - services/api/go.sum
  - pkg/domain/errors.go
  - services/api/internal/repository/conversation.go
  - services/api/internal/repository/message.go
  - services/api/internal/repository/search_indexes.go
  - services/api/internal/repository/search_indexes_test.go
  - services/api/internal/repository/search_messages_test.go
  - services/api/internal/service/search.go
  - services/api/internal/service/search_test.go
  - services/api/internal/service/snippet.go
  - services/api/internal/service/snippet_test.go
  - services/api/internal/handler/search.go
  - services/api/internal/handler/search_test.go
  - services/api/internal/router/router.go
  - services/api/cmd/main.go
  - test/integration/search_test.go
autonomous: true
requirements:
  - SEARCH-01
  - SEARCH-02
  - SEARCH-03
  - SEARCH-05
  - SEARCH-06
  - SEARCH-07
threat_model_summary: "Three named threats: T-19-CROSS-TENANT (HIGH) — search returning messages from another business/user; T-19-INDEX-503 (MEDIUM) — /search returning 500 because indexes not built; T-19-LOG-LEAK (HIGH) — query text leaking into logs. All mitigated by repo signature, atomic.Bool readiness, and slog metadata-only field shape."
must_haves:
  truths:
    - "pkg/security/pii.go is NOT used here — search logs are metadata-only by field-shape contract (SEARCH-07)"
    - "Russian UI copy is NOT touched here (backend); D-13 / search empty-state row lives in 19-04"
    - "All Go modules use `replace github.com/f1xgun/onevoice/pkg => ../../pkg`"
    - "t.Setenv not os.Setenv in tests"
    - "github.com/kljensen/snowball v0.10.0 (MIT, pure Go) is the chosen Russian stemmer (RESEARCH §1)"
    - "$text MUST be in the FIRST $match stage of the aggregation pipeline (Mongo manual rule — RESEARCH §5)"
    - "Search results aggregated per conversation: title + top-scored snippet + match_count + aggregated_score (max(titleScore × 20, contentScore × 10)); single-match conversations omit the +N badge (D-07)"
    - "Two-phase query strategy is REQUIRED, not optional — Message has no business_id field (verified pkg/domain/mongo_models.go:52-70)"
    - "EnsureSearchIndexes is wired into main.go startup BEFORE searcher.MarkIndexesReady() — atomic.Bool flag flips only after index creation succeeds"
    - "Serialization note: 19-02 Task 2 and 19-03 Task 4 both modify services/api/cmd/main.go; 19-03 depends_on: [19-02], so 19-03 Task 4 commits AFTER 19-02 Task 2 — V19 backfill is in place when search wiring lands; both blocks coexist in main.go startup"
    - "ErrInvalidScope is returned IMMEDIATELY by Searcher.Search when businessID == \"\" || userID == \"\" — defense-in-depth (Pitfalls §19)"
    - "Cross-tenant integration test in test/integration/search_test.go is BLOCKING — proves SEARCH-02 contract"
    - "Search logs metadata only: {user_id, business_id, query_length} — never the query text (SEARCH-07)"
    - "Backend computes [start, end] byte ranges for stem-matched tokens via github.com/kljensen/snowball — frontend wraps each in <mark> (D-09)"
    - "GET /api/v1/search returns 503 + Retry-After: 5 until searchReady atomic.Bool flips true (SEARCH-06)"
  artifacts:
    - path: services/api/go.mod
      provides: "github.com/kljensen/snowball v0.10.0 dependency line"
      contains: "kljensen/snowball"
    - path: pkg/domain/errors.go
      provides: "domain.ErrInvalidScope and domain.ErrSearchIndexNotReady sentinels"
      contains: "ErrInvalidScope"
    - path: services/api/internal/repository/search_indexes.go
      provides: "EnsureSearchIndexes function creating two text indexes idempotently"
      contains: "EnsureSearchIndexes"
    - path: services/api/internal/repository/conversation.go
      provides: "SearchTitles (text query) + ScopedConversationIDs (allowlist for two-phase)"
      contains: "SearchTitles"
    - path: services/api/internal/repository/message.go
      provides: "SearchByConversationIDs aggregation pipeline"
      contains: "SearchByConversationIDs"
    - path: services/api/internal/service/search.go
      provides: "Searcher orchestration (two-phase + merge + rank); ErrInvalidScope guard; indexReady flag"
      contains: "func (s *Searcher) Search("
    - path: services/api/internal/service/snippet.go
      provides: "BuildSnippet + HighlightRanges pure helpers using snowball stemmer"
      contains: "BuildSnippet"
    - path: services/api/internal/handler/search.go
      provides: "GET /api/v1/search handler with 400/401/503/200 mapping; metadata-only logs"
      contains: "func (h *SearchHandler) Search("
    - path: test/integration/search_test.go
      provides: "TestSearchCrossTenant + TestSearch_503BeforeReady + TestSearchAggregatedShape integration tests"
      contains: "TestSearchCrossTenant"
  key_links:
    - from: services/api/cmd/main.go
      to: services/api/internal/repository/search_indexes.go
      via: EnsureSearchIndexes called BEFORE searcher.MarkIndexesReady()
      pattern: "EnsureSearchIndexes"
    - from: services/api/internal/handler/search.go
      to: services/api/internal/service/search.go
      via: errors.Is(err, domain.ErrSearchIndexNotReady) → 503 + Retry-After
      pattern: "ErrSearchIndexNotReady"
    - from: services/api/internal/service/search.go
      to: services/api/internal/repository/conversation.go
      via: SearchTitles + ScopedConversationIDs (phase 1)
      pattern: "SearchTitles"
    - from: services/api/internal/service/search.go
      to: services/api/internal/repository/message.go
      via: SearchByConversationIDs (phase 2)
      pattern: "SearchByConversationIDs"
    - from: services/api/internal/service/snippet.go
      to: github.com/kljensen/snowball/russian
      via: russian.Stem(token, false)
      pattern: "russian.Stem"
---

<objective>
Implement the Phase 19 search backend in three layers: repository (text indexes + SearchTitles + SearchByConversationIDs aggregation), service (Searcher orchestrating two-phase query + snippet builder + snowball highlight ranges + ErrInvalidScope guard + atomic.Bool readiness flag), and handler (`GET /api/v1/search?q=&project_id=&limit=20` with bearer auth + 400/401/503/200 mapping + metadata-only slog). Wave-0 install `github.com/kljensen/snowball v0.10.0`. Wire `EnsureSearchIndexes` into `services/api/cmd/main.go` startup BEFORE `searcher.MarkIndexesReady()` flips the flag. Land a BLOCKING cross-tenant integration test in `test/integration/search_test.go` proving Business B's messages never leak to Business A's search.

Purpose: SEARCH-01 (idempotent text indexes), SEARCH-02 (repo signature requires (business_id, user_id), rejects empty), SEARCH-03 (aggregate by conversation, snippet ±40-120, ranked), SEARCH-05 (project filter), SEARCH-06 (atomic.Bool readiness gate before endpoint enabled), SEARCH-07 (metadata-only logging).

Output: Search backend stack + integration test suite + 3 named threats mitigated.
</objective>

<execution_context>
@/Users/f1xgun/onevoice/.claude/get-shit-done/workflows/execute-plan.md
@/Users/f1xgun/onevoice/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/PROJECT.md
@.planning/ROADMAP.md
@.planning/STATE.md
@.planning/phases/19-search-sidebar-redesign/19-CONTEXT.md
@.planning/phases/19-search-sidebar-redesign/19-RESEARCH.md
@.planning/phases/19-search-sidebar-redesign/19-PATTERNS.md
@.planning/phases/19-search-sidebar-redesign/19-VALIDATION.md
@services/api/AGENTS.md
@pkg/AGENTS.md
@docs/api-design.md
@docs/security.md
@docs/golden-principles.md
@docs/go-style.md
@docs/go-patterns.md

<interfaces>
<!-- Verified Mongo driver version (RESEARCH §4): -->
go.mongodb.org/mongo-driver/v2 v2.5.0 — supports SetDefaultLanguage, SetWeights, $text + $meta:textScore.

<!-- Existing services/api/internal/repository/conversation.go signatures: -->
- `func EnsureConversationIndexes(ctx context.Context, db *mongo.Database) error` (lines 224–243)
- `func (r *conversationRepository) Find(...)` patterns at lines 55–74

<!-- Existing services/api/internal/repository/message.go: -->
- Message struct lines 52–70 in pkg/domain/mongo_models.go — fields: ID, ConversationID, Role, Content, Status, CreatedAt. NO BusinessID. NO UserID.
- `func (r *messageRepository) FindByConversationActive(...)` lines 95–115 — closest analog for filter+decode shape.

<!-- Snowball lib (RESEARCH §1): -->
github.com/kljensen/snowball/russian — `func Stem(word string, stemStopWords bool) string`

<!-- Existing pkg/domain/errors.go (find via grep — likely contains ErrConversationNotFound, ErrMessageNotFound, ErrIntegrationNotFound, etc.). Add: -->
- `ErrInvalidScope = errors.New("search: invalid scope (business_id/user_id required)")`
- `ErrSearchIndexNotReady = errors.New("search: index not ready")`

<!-- Existing services/api/internal/middleware/auth.go: -->
- `middleware.GetUserID(ctx) (uuid.UUID, error)` — verified PATTERNS §7 line 446 (NOT UserIDFromContext)

<!-- Existing services/api/internal/handler/response.go (find via grep): -->
- `writeJSON(w, status, body)`, `writeJSONError(w, status, msg)` — package-level helpers

<!-- Existing test/integration/main_test.go: -->
- `setupTestUser(t, email, password) string` — returns access token
- `setupTestBusiness(t, accessToken)` — creates business
- `cleanupDatabase(t)` — wipes Mongo + Postgres
</interfaces>
</context>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1 [BLOCKING — Wave 0]: Install snowball lib + add ErrInvalidScope/ErrSearchIndexNotReady sentinels + scaffold integration test + scaffold all repo/service/handler test files</name>
  <files>services/api/go.mod, services/api/go.sum, pkg/domain/errors.go, pkg/domain/errors_test.go, services/api/internal/repository/search_indexes_test.go, services/api/internal/repository/search_messages_test.go, services/api/internal/service/search_test.go, services/api/internal/service/snippet_test.go, services/api/internal/handler/search_test.go, test/integration/search_test.go</files>
  <read_first>
    - services/api/go.mod (verify current dependency block)
    - pkg/domain/errors.go (find existing sentinel error pattern; e.g., `var ErrConversationNotFound = errors.New(...)`)
    - test/integration/authorization_test.go lines 1–194 (canonical two-user pattern; copy structure for new search_test.go)
    - test/integration/conversation_test.go lines 14–93 (CreateConversation request shape)
    - test/integration/main_test.go (find setupTestUser, setupTestBusiness, cleanupDatabase signatures)
    - services/api/internal/service/titler_test.go lines 1–80 (fakeRouter + nil-embedded fakeRepo + captureLogs — pattern templates)
    - .planning/phases/19-search-sidebar-redesign/19-RESEARCH.md §1 (snowball install lines 76–80)
    - .planning/phases/19-search-sidebar-redesign/19-RESEARCH.md §6 (cross-tenant test lines 482–558)
    - .planning/phases/19-search-sidebar-redesign/19-VALIDATION.md (Wave 0 list lines 60–72)
  </read_first>
  <action>
1. Add the snowball dependency:
   ```bash
   cd services/api
   GOWORK=off go get github.com/kljensen/snowball@v0.10.0
   GOWORK=off go mod tidy
   ```
   Verify `go.mod` contains a `require` line for `github.com/kljensen/snowball v0.10.0`. Verify `go.sum` is updated. The `replace github.com/f1xgun/onevoice/pkg => ../../pkg` directive at the top of `go.mod` MUST remain unchanged.

2. Edit `pkg/domain/errors.go` (find via grep `var ErrConversationNotFound`). Append:
   ```go
   // Phase 19 search sentinels.
   // ErrInvalidScope is returned by SearchService.Search when businessID or userID is empty.
   // Defense-in-depth: prevents accidental "search across all tenants" if upstream forgets to scope.
   ErrInvalidScope = errors.New("search: invalid scope (business_id and user_id required)")

   // ErrSearchIndexNotReady is returned while EnsureSearchIndexes has not completed at startup.
   // Maps to HTTP 503 + Retry-After: 5 in the search handler (SEARCH-06).
   ErrSearchIndexNotReady = errors.New("search: index not ready")
   ```

   If the existing file groups errors via `var (...)` block, add inside the block.

3. Add `pkg/domain/errors_test.go` (or extend if exists) verifying these are non-nil error values:
   ```go
   func TestPhase19SearchErrors(t *testing.T) {
       require.NotNil(t, ErrInvalidScope)
       require.NotNil(t, ErrSearchIndexNotReady)
       require.True(t, errors.Is(ErrInvalidScope, ErrInvalidScope))
   }
   ```

4. Create scaffolds — each must FAIL on `go test` (RED state) because target functions don't exist yet:

   **`services/api/internal/repository/search_indexes_test.go`** — assert `repository.EnsureSearchIndexes(ctx, db)` exists; second call returns nil (idempotency). Use the existing `setupTestMongo` helper (find via grep in repository test files).

   **`services/api/internal/repository/search_messages_test.go`** — assert `messageRepository.SearchByConversationIDs(ctx, query, convIDs, limit)` exists. Single test that seeds 3 messages across 2 conversations + asserts the aggregation returns 2 rows (one per conversation).

   **`services/api/internal/service/search_test.go`** — assert:
   - Empty `businessID` → `ErrInvalidScope`
   - Empty `userID` → `ErrInvalidScope`
   - `indexReady = false` → `ErrSearchIndexNotReady`
   - Log capture (titler_test.go captureLogs pattern at lines 64–71): captured log line contains `query_length` AND does NOT contain the actual query text bytes (SEARCH-07).

   **`services/api/internal/service/snippet_test.go`** — table-driven cases for `BuildSnippet` per RESEARCH §10 lines 967–988 (3 cases: middle match with both ellipses, near-start match, no match).

   **`services/api/internal/handler/search_test.go`** — assert:
   - `q=""` → 400
   - `q="a"` (len < 2) → 400
   - missing bearer → 401
   - `searchReady=false` → 503 + `Retry-After: 5`
   - successful search → 200 + log captures `query_length`, NOT query text.

5. Create `test/integration/search_test.go` from RESEARCH §6 sketch (lines 489–558). Skeleton with these subtests:
   - `TestSearchCrossTenant` containing `UserASearchesInvoiceSeesOnlyOwn` and `UserBSearchesInvoiceSeesOnlyOwn`
   - `TestSearchEmptyQueryReturns400`
   - `TestSearchMissingBearerReturns401`
   - `TestSearch_503BeforeReady` — flagged with a comment `// TODO: stub readiness flag toggle once handler is wired`. May be a `t.Skip` if the readiness toggle isn't easily testable from integration; planner picks at execution time.
   - `TestSearchAggregatedShape` — assert response is `[]SearchResultRow` keyed by `conversationId`, not raw messages.
   - `TestSearchProjectScope` — `?project_id=…` filters out other-project hits.

   Add helper `createConversationWithMessage(t, token, title, msg) string` to `test/integration/main_test.go` (or wherever `setupTestUser` lives — find via grep). Concrete shape:
   ```go
   func createConversationWithMessage(t *testing.T, token, title, msg string) string {
       t.Helper()
       // POST /api/v1/conversations with Authorization: Bearer <token> and body {title, projectId: null}
       // POST /api/v1/chat/{id} with body {message: msg} and consume the SSE stream to completion (or call the test-only direct insert path if one exists)
       // Return the conversation ID.
   }
   ```

6. Verify scaffolds compile (verify command runs `go vet` only — NOT `go test`). The scaffolds are intentionally RED because they reference symbols defined in Tasks 2–4. Acceptance for THIS task is file-existence + `go vet ./...` exits 0; `go test` is reserved for Tasks 2–4 where each scaffold turns GREEN as its target lands.
   ```bash
   cd services/api && GOWORK=off go vet ./...        # MUST pass — files compile
   ```

   If `go vet` fails, fix the scaffold imports.
  </action>
  <verify>
    <automated>cd services/api && GOWORK=off go mod tidy && grep -q "github.com/kljensen/snowball" go.mod && cd /Users/f1xgun/onevoice/.worktrees/milestone-1.3 && grep -q "ErrInvalidScope" pkg/domain/errors.go && grep -q "ErrSearchIndexNotReady" pkg/domain/errors.go && test -f services/api/internal/repository/search_indexes_test.go && test -f services/api/internal/repository/search_messages_test.go && test -f services/api/internal/service/search_test.go && test -f services/api/internal/service/snippet_test.go && test -f services/api/internal/handler/search_test.go && test -f test/integration/search_test.go && cd services/api && GOWORK=off go vet ./...</automated>
  </verify>
  <acceptance_criteria>
    - `services/api/go.mod` contains `github.com/kljensen/snowball v0.10.0` (or any v0.10.x)
    - `services/api/go.mod` STILL contains `replace github.com/f1xgun/onevoice/pkg => ../../pkg`
    - `pkg/domain/errors.go` contains `ErrInvalidScope` AND `ErrSearchIndexNotReady`
    - 5 scaffold test files exist (search_indexes_test, search_messages_test, search_test (service), snippet_test, search_test (handler))
    - `test/integration/search_test.go` exists and contains `TestSearchCrossTenant`
    - `cd services/api && GOWORK=off go vet ./...` exits 0 (scaffolds compile)
  </acceptance_criteria>
  <done>Snowball lib installed; sentinels added; 5 scaffold test files + integration test suite skeleton committed; Wave-0 RED state achieved.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: Repository layer — text indexes (EnsureSearchIndexes), SearchTitles, SearchByConversationIDs aggregation pipeline</name>
  <files>services/api/internal/repository/search_indexes.go, services/api/internal/repository/conversation.go, services/api/internal/repository/message.go</files>
  <read_first>
    - services/api/internal/repository/conversation.go lines 224–243 (EnsureConversationIndexes — extension target; ALSO from 19-02 should already include the new compound index — verify it landed)
    - services/api/internal/repository/message.go lines 95–115 (FindByConversationActive — filter+decode shape analog)
    - services/api/internal/repository/pending_tool_call.go lines 62–94 (EnsurePendingToolCallsIndexes — alternate index-creation analog cited in RESEARCH §13)
    - .planning/phases/19-search-sidebar-redesign/19-RESEARCH.md §4 (lines 261–357 — concrete EnsureSearchIndexes function with two IndexModel + idempotent IsDuplicateKeyError swallow + background:true note)
    - .planning/phases/19-search-sidebar-redesign/19-RESEARCH.md §5 (lines 359–479 — two-phase pipeline; phase-1 Find vs phase-2 Aggregate)
    - .planning/phases/19-search-sidebar-redesign/19-PATTERNS.md §2 + §3 (concrete code snippets for SearchTitles + SearchByConversationIDs)
  </read_first>
  <behavior>
    - Test 1 (EnsureSearchIndexes): Two text indexes are created with names `conversations_title_text_v19` and `messages_content_text_v19` (or planner-chosen names — must include "text" and "v19" or similar marker for grep audit).
    - Test 2 (EnsureSearchIndexes idempotent): Calling twice succeeds (second call swallows IsDuplicateKeyError or no-op).
    - Test 3 (EnsureSearchIndexes options): Both indexes use `SetDefaultLanguage("russian")`. Title index has `SetWeights({title: 20})`. Content index has `SetWeights({content: 10})`. Both call `SetBackground(true)`.
    - Test 4 (SearchTitles): Query for `«инвойс»` against a Conversation collection containing one matching title returns 1 hit; collection scoped by `(user_id, business_id)` rejects mismatched scope.
    - Test 5 (SearchTitles project filter): With `projectID = &"proj-1"`, only conversations matching that project_id are returned.
    - Test 6 (ScopedConversationIDs): Returns IDs of all conversations within `(user_id, business_id, projectID?)` capped at MaxScopedConversations (1000) with most-recent-by-last_message_at ordering.
    - Test 7 (SearchByConversationIDs): Aggregation pipeline returns one row per conversation (grouped), with top_message_id, top_content, top_score, match_count.
    - Test 8 (SearchByConversationIDs): With `convIDs = []`, returns empty slice without error.
    - Test 9 (SearchByConversationIDs): Cap convIDs at 1000 — if `len(convIDs) > 1000`, log warning + truncate to 1000 newest.
  </behavior>
  <action>
1. Create `services/api/internal/repository/search_indexes.go` (NEW). Copy verbatim from RESEARCH §4 lines 277–311:
   ```go
   package repository

   import (
       "context"
       "fmt"

       "go.mongodb.org/mongo-driver/v2/bson"
       "go.mongodb.org/mongo-driver/v2/mongo"
       "go.mongodb.org/mongo-driver/v2/mongo/options"
   )

   // EnsureSearchIndexes creates the v1.3 text search indexes idempotently at API startup.
   // SetBackground(true) is a no-op on Mongo 4.2+; kept for source-readability + SEARCH-06 wording.
   // The two indexes are independent: one on conversations.title (weight 20), one on messages.content (weight 10).
   // Both use default_language: "russian" for proper Cyrillic stemming (Pitfalls §17).
   func EnsureSearchIndexes(ctx context.Context, db *mongo.Database) error {
       convs := db.Collection("conversations")
       msgs := db.Collection("messages")

       titleIdx := mongo.IndexModel{
           Keys: bson.D{{Key: "title", Value: "text"}},
           Options: options.Index().
               SetName("conversations_title_text_v19").
               SetDefaultLanguage("russian").
               SetWeights(bson.D{{Key: "title", Value: 20}}).
               SetBackground(true),
       }
       if _, err := convs.Indexes().CreateOne(ctx, titleIdx); err != nil {
           if !mongo.IsDuplicateKeyError(err) {
               return fmt.Errorf("ensure conversations title text index: %w", err)
           }
       }

       contentIdx := mongo.IndexModel{
           Keys: bson.D{{Key: "content", Value: "text"}},
           Options: options.Index().
               SetName("messages_content_text_v19").
               SetDefaultLanguage("russian").
               SetWeights(bson.D{{Key: "content", Value: 10}}).
               SetBackground(true),
       }
       if _, err := msgs.Indexes().CreateOne(ctx, contentIdx); err != nil {
           if !mongo.IsDuplicateKeyError(err) {
               return fmt.Errorf("ensure messages content text index: %w", err)
           }
       }
       return nil
   }
   ```

2. Edit `services/api/internal/repository/conversation.go`. Add three new methods on `conversationRepository` and add their interface signatures to `pkg/domain.ConversationRepository`:

   **SearchTitles** (concrete code from RESEARCH §4 lines 316–341):
   ```go
   // SearchTitles runs the phase-1 $text query against conversations.title.
   // Returns title hits + the corresponding conversation IDs (callers may use the IDs to bound phase-2).
   // Empty businessID or userID returns ErrInvalidScope.
   func (r *conversationRepository) SearchTitles(
       ctx context.Context,
       businessID, userID, query string,
       projectID *string,
       limit int,
   ) ([]ConversationTitleHit, []string, error) {
       if businessID == "" || userID == "" {
           return nil, nil, domain.ErrInvalidScope
       }
       filter := bson.M{
           "$text":       bson.M{"$search": query},
           "user_id":     userID,
           "business_id": businessID,
       }
       if projectID != nil {
           filter["project_id"] = *projectID
       }
       opts := options.Find().
           SetProjection(bson.M{
               "score":           bson.M{"$meta": "textScore"},
               "title":           1,
               "project_id":      1,
               "user_id":         1,
               "business_id":     1,
               "last_message_at": 1,
           }).
           SetSort(bson.D{{Key: "score", Value: bson.M{"$meta": "textScore"}}}).
           SetLimit(int64(limit))

       cursor, err := r.collection.Find(ctx, filter, opts)
       if err != nil {
           return nil, nil, fmt.Errorf("search titles: %w", err)
       }
       defer func() { _ = cursor.Close(ctx) }()

       var hits []ConversationTitleHit
       if err := cursor.All(ctx, &hits); err != nil {
           return nil, nil, fmt.Errorf("decode title hits: %w", err)
       }
       ids := make([]string, len(hits))
       for i, h := range hits {
           ids[i] = h.ID
       }
       return hits, ids, nil
   }

   type ConversationTitleHit struct {
       ID            string     `bson:"_id"`
       Title         string     `bson:"title"`
       ProjectID     *string    `bson:"project_id"`
       UserID        string     `bson:"user_id"`
       BusinessID    string     `bson:"business_id"`
       Score         float64    `bson:"score"`
       LastMessageAt *time.Time `bson:"last_message_at"`
   }
   ```

   **ScopedConversationIDs** — broader allowlist for phase-2's `$in` (returns ALL conversations visible to scope, not just title-matching ones):
   ```go
   const MaxScopedConversations = 1000

   func (r *conversationRepository) ScopedConversationIDs(
       ctx context.Context,
       businessID, userID string,
       projectID *string,
   ) ([]string, error) {
       if businessID == "" || userID == "" {
           return nil, domain.ErrInvalidScope
       }
       filter := bson.M{"user_id": userID, "business_id": businessID}
       if projectID != nil {
           filter["project_id"] = *projectID
       }
       opts := options.Find().
           SetProjection(bson.M{"_id": 1}).
           SetSort(bson.D{{Key: "last_message_at", Value: -1}}).
           SetLimit(MaxScopedConversations + 1)
       cursor, err := r.collection.Find(ctx, filter, opts)
       if err != nil {
           return nil, fmt.Errorf("scoped conversation ids: %w", err)
       }
       defer func() { _ = cursor.Close(ctx) }()
       var ids []struct{ ID string `bson:"_id"` }
       if err := cursor.All(ctx, &ids); err != nil {
           return nil, fmt.Errorf("decode scoped ids: %w", err)
       }
       if len(ids) > MaxScopedConversations {
           slog.WarnContext(ctx, "search: scoped conversation set exceeds cap",
               "user_id", userID, "business_id", businessID, "count", len(ids), "cap", MaxScopedConversations)
           ids = ids[:MaxScopedConversations]
       }
       out := make([]string, len(ids))
       for i, x := range ids {
           out[i] = x.ID
       }
       return out, nil
   }
   ```

3. Edit `services/api/internal/repository/message.go`. Add `SearchByConversationIDs` from RESEARCH §5 lines 391–438:
   ```go
   type MessageSearchHit struct {
       ConversationID string  `bson:"_id"`
       TopMessageID   string  `bson:"top_message_id"`
       TopContent     string  `bson:"top_content"`
       TopScore       float64 `bson:"top_score"`
       MatchCount     int     `bson:"match_count"`
   }

   func (r *messageRepository) SearchByConversationIDs(
       ctx context.Context,
       query string,
       convIDs []string,
       limit int,
   ) ([]MessageSearchHit, error) {
       if len(convIDs) == 0 {
           return []MessageSearchHit{}, nil
       }
       if len(convIDs) > 1000 {
           slog.WarnContext(ctx, "search: convIDs > 1000, truncating", "count", len(convIDs))
           convIDs = convIDs[:1000]
       }
       pipeline := mongo.Pipeline{
           bson.D{{Key: "$match", Value: bson.M{
               "$text":           bson.M{"$search": query},
               "conversation_id": bson.M{"$in": convIDs},
           }}},
           bson.D{{Key: "$addFields", Value: bson.M{"score": bson.M{"$meta": "textScore"}}}},
           bson.D{{Key: "$sort", Value: bson.D{{Key: "score", Value: -1}}}},
           bson.D{{Key: "$group", Value: bson.D{
               {Key: "_id", Value: "$conversation_id"},
               {Key: "top_message_id", Value: bson.D{{Key: "$first", Value: "$_id"}}},
               {Key: "top_content", Value: bson.D{{Key: "$first", Value: "$content"}}},
               {Key: "top_score", Value: bson.D{{Key: "$first", Value: "$score"}}},
               {Key: "match_count", Value: bson.D{{Key: "$sum", Value: 1}}},
           }}},
           bson.D{{Key: "$sort", Value: bson.D{{Key: "top_score", Value: -1}}}},
           bson.D{{Key: "$limit", Value: int64(limit)}},
       }
       cur, err := r.collection.Aggregate(ctx, pipeline)
       if err != nil {
           return nil, fmt.Errorf("search messages aggregate: %w", err)
       }
       defer func() { _ = cur.Close(ctx) }()
       var hits []MessageSearchHit
       if err := cur.All(ctx, &hits); err != nil {
           return nil, fmt.Errorf("decode search hits: %w", err)
       }
       return hits, nil
   }
   ```

4. Add the three new methods to the `pkg/domain.ConversationRepository` and `pkg/domain.MessageRepository` interfaces (find via grep `type ConversationRepository interface` and `type MessageRepository interface`):
   ```go
   // ConversationRepository:
   SearchTitles(ctx context.Context, businessID, userID, query string, projectID *string, limit int) ([]repository.ConversationTitleHit, []string, error)
   ScopedConversationIDs(ctx context.Context, businessID, userID string, projectID *string) ([]string, error)

   // MessageRepository:
   SearchByConversationIDs(ctx context.Context, query string, convIDs []string, limit int) ([]repository.MessageSearchHit, error)
   ```

   If exposing `repository.ConversationTitleHit` from `pkg/domain` creates an import cycle, move the result types into `pkg/domain` instead (the planner picks based on the actual import graph).

5. Fill out the previously-scaffolded test files with concrete cases that pass against the new implementations:
   - `search_indexes_test.go` — Tests 1–3
   - Extend `conversation_test.go` (or add `search_titles_test.go`) — Tests 4–6
   - `search_messages_test.go` — Tests 7–9

6. Run:
   ```bash
   cd services/api && GOWORK=off go build ./...
   cd services/api && GOWORK=off go test -race ./internal/repository/... -run "TestEnsureSearchIndexes|TestSearchTitles|TestScopedConversationIDs|TestSearchByConversationIDs"
   ```

   Both must exit 0.
  </action>
  <verify>
    <automated>cd services/api && GOWORK=off go build ./... && GOWORK=off go test -race ./internal/repository/... -run "TestEnsureSearchIndexes|TestSearchTitles|TestScopedConversationIDs|TestSearchByConversationIDs"</automated>
  </verify>
  <acceptance_criteria>
    - File exists: `services/api/internal/repository/search_indexes.go`
    - File contains `func EnsureSearchIndexes(`
    - `grep -c "default_language" services/api/internal/repository/search_indexes.go` >= 2 (note: actual code uses `SetDefaultLanguage("russian")` — adjust grep to that string)
    - `grep -c 'SetDefaultLanguage("russian")' services/api/internal/repository/search_indexes.go` >= 2
    - `grep -c "SetWeights" services/api/internal/repository/search_indexes.go` >= 2
    - `services/api/internal/repository/conversation.go` contains `func (r *conversationRepository) SearchTitles(` AND `func (r *conversationRepository) ScopedConversationIDs(`
    - `services/api/internal/repository/message.go` contains `func (r *messageRepository) SearchByConversationIDs(`
    - `services/api/internal/repository/message.go` contains `mongo.Pipeline` AND `$text` AND `$group` AND `$addFields`
    - `cd services/api && GOWORK=off go build ./...` exits 0
    - `cd services/api && GOWORK=off go test -race ./internal/repository/... -run "TestEnsureSearchIndexes|TestSearchTitles|TestScopedConversationIDs|TestSearchByConversationIDs"` exits 0
  </acceptance_criteria>
  <done>EnsureSearchIndexes + SearchTitles + ScopedConversationIDs + SearchByConversationIDs implemented; tests GREEN.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 3: Service layer — Searcher orchestration + BuildSnippet/HighlightRanges with snowball</name>
  <files>services/api/internal/service/search.go, services/api/internal/service/snippet.go</files>
  <read_first>
    - services/api/internal/service/titler.go lines 60–273 (constructor panics, slog metadata, multi-step pipeline, sanitize helpers)
    - .planning/phases/19-search-sidebar-redesign/19-RESEARCH.md §1 (lines 90–129 — HighlightRanges sketch)
    - .planning/phases/19-search-sidebar-redesign/19-RESEARCH.md §5 (lines 441–477 — Searcher.Search orchestration)
    - .planning/phases/19-search-sidebar-redesign/19-RESEARCH.md §10 (lines 870–963 — BuildSnippet + firstStemMatch + expandLeftToBoundary + expandRightToBoundary)
    - .planning/phases/19-search-sidebar-redesign/19-PATTERNS.md §5 + §6 (Searcher + snippet helpers patterns)
  </read_first>
  <behavior>
    - Test 1: `Searcher.Search` with `businessID = ""` → returns `domain.ErrInvalidScope` immediately, no repo calls made (use spy).
    - Test 2: `Searcher.Search` with `userID = ""` → returns `domain.ErrInvalidScope` immediately.
    - Test 3: `indexReady = false` → returns `domain.ErrSearchIndexNotReady`.
    - Test 4: Successful path: calls `convRepo.SearchTitles` and `convRepo.ScopedConversationIDs` and `msgRepo.SearchByConversationIDs`, merges results via `mergeAndRank` with weights (20, 10), returns `[]SearchResult` keyed by conversation.
    - Test 5: `BuildSnippet` returns `«…Я хочу запланировать пост в Telegram на пятницу вечером, чтобы охватить аудиторию…»` for the canonical input from RESEARCH §10 first table case.
    - Test 6: `BuildSnippet` returns full content (no ellipses) for short content < 120 chars.
    - Test 7: `BuildSnippet` returns empty string when no stem match.
    - Test 8: `HighlightRanges` for input `«запланировать пост»` with stem `«запланирова»` returns `[[0, byte_end_of_zaplanirovat]]`.
    - Test 9: Log capture: every Searcher.Search log line carries `query_length`, `user_id`, `business_id`. Captured buffer NEVER contains the actual `q` text bytes (assert via `bytes.Contains(buf, []byte(literalQuery)) == false`).
  </behavior>
  <action>
1. Create `services/api/internal/service/snippet.go` (NEW). Copy verbatim from RESEARCH §10 lines 870–963:
   ```go
   package service

   import (
       "strings"
       "unicode"

       "github.com/kljensen/snowball/russian"
   )

   func BuildSnippet(content string, queryStems map[string]struct{}) string {
       matchStart, matchEnd := firstStemMatch(content, queryStems)
       if matchStart < 0 {
           return ""
       }
       const halfWindow = 50
       desired := matchStart - halfWindow
       if desired < 0 {
           desired = 0
       } else {
           desired = expandLeftToBoundary(content, desired)
       }
       end := matchEnd + halfWindow
       if end > len(content) {
           end = len(content)
       } else {
           end = expandRightToBoundary(content, end)
       }
       snippet := content[desired:end]
       if desired > 0 {
           snippet = "…" + snippet
       }
       if end < len(content) {
           snippet = snippet + "…"
       }
       return snippet
   }

   func firstStemMatch(content string, queryStems map[string]struct{}) (int, int) {
       runes := []rune(content)
       pos := 0
       for pos < len(runes) {
           for pos < len(runes) && !unicode.IsLetter(runes[pos]) {
               pos++
           }
           start := pos
           for pos < len(runes) && unicode.IsLetter(runes[pos]) {
               pos++
           }
           if start == pos {
               continue
           }
           token := strings.ToLower(string(runes[start:pos]))
           if _, hit := queryStems[russian.Stem(token, false)]; hit {
               byteStart := len(string(runes[:start]))
               byteEnd := len(string(runes[:pos]))
               return byteStart, byteEnd
           }
       }
       return -1, -1
   }

   func expandLeftToBoundary(s string, pos int) int {
       for pos > 0 && !unicode.IsSpace(rune(s[pos-1])) {
           pos--
       }
       return pos
   }

   func expandRightToBoundary(s string, pos int) int {
       for pos < len(s) && !unicode.IsSpace(rune(s[pos])) {
           pos++
       }
       return pos
   }

   // HighlightRanges returns byte ranges in `snippet` where any token's stem matches any query stem.
   // Stable order, non-overlapping.
   func HighlightRanges(snippet string, queryStems map[string]struct{}) [][2]int {
       var marks [][2]int
       runes := []rune(snippet)
       pos := 0
       for pos < len(runes) {
           for pos < len(runes) && !unicode.IsLetter(runes[pos]) {
               pos++
           }
           start := pos
           for pos < len(runes) && unicode.IsLetter(runes[pos]) {
               pos++
           }
           if start == pos {
               continue
           }
           token := strings.ToLower(string(runes[start:pos]))
           if _, hit := queryStems[russian.Stem(token, false)]; hit {
               byteStart := len(string(runes[:start]))
               byteEnd := len(string(runes[:pos]))
               marks = append(marks, [2]int{byteStart, byteEnd})
           }
       }
       return marks
   }

   // QueryStems builds the set of stems used by BuildSnippet and HighlightRanges.
   func QueryStems(query string) map[string]struct{} {
       result := make(map[string]struct{})
       runes := []rune(query)
       pos := 0
       for pos < len(runes) {
           for pos < len(runes) && !unicode.IsLetter(runes[pos]) {
               pos++
           }
           start := pos
           for pos < len(runes) && unicode.IsLetter(runes[pos]) {
               pos++
           }
           if start == pos {
               continue
           }
           token := strings.ToLower(string(runes[start:pos]))
           result[russian.Stem(token, false)] = struct{}{}
       }
       return result
   }
   ```

2. Create `services/api/internal/service/search.go` (NEW). Concrete code (RESEARCH §5 + §7):
   ```go
   package service

   import (
       "context"
       "log/slog"
       "sort"
       "sync/atomic"

       "github.com/f1xgun/onevoice/pkg/domain"
       "github.com/f1xgun/onevoice/services/api/internal/repository"
   )

   type SearchResult struct {
       ConversationID string             `json:"conversationId"`
       Title          string             `json:"title"`
       ProjectID      *string            `json:"projectId,omitempty"`
       Snippet        string             `json:"snippet"`
       MatchCount     int                `json:"matchCount"`
       TopMessageID   string             `json:"topMessageId,omitempty"`
       Score          float64            `json:"score"`
       Marks          [][2]int           `json:"marks,omitempty"`
       LastMessageAt  *time.Time         `json:"lastMessageAt,omitempty"`
   }

   type Searcher struct {
       convRepo   domain.ConversationRepository
       msgRepo    domain.MessageRepository
       indexReady *atomic.Bool
   }

   func NewSearcher(convRepo domain.ConversationRepository, msgRepo domain.MessageRepository) *Searcher {
       if convRepo == nil {
           panic("NewSearcher: convRepo cannot be nil")
       }
       if msgRepo == nil {
           panic("NewSearcher: msgRepo cannot be nil")
       }
       return &Searcher{convRepo: convRepo, msgRepo: msgRepo, indexReady: &atomic.Bool{}}
   }

   func (s *Searcher) MarkIndexesReady() { s.indexReady.Store(true) }

   func (s *Searcher) Search(ctx context.Context, businessID, userID, query string, projectID *string, limit int) ([]SearchResult, error) {
       // Defense-in-depth (Pitfalls §19, T-19-CROSS-TENANT mitigation).
       if businessID == "" || userID == "" {
           return nil, domain.ErrInvalidScope
       }
       if !s.indexReady.Load() {
           return nil, domain.ErrSearchIndexNotReady
       }

       slog.InfoContext(ctx, "search.query",
           "user_id", userID,
           "business_id", businessID,
           "query_length", len(query),
       )

       titleHits, _, err := s.convRepo.SearchTitles(ctx, businessID, userID, query, projectID, limit)
       if err != nil {
           return nil, err
       }
       scopedIDs, err := s.convRepo.ScopedConversationIDs(ctx, businessID, userID, projectID)
       if err != nil {
           return nil, err
       }
       msgHits, err := s.msgRepo.SearchByConversationIDs(ctx, query, scopedIDs, limit*2)
       if err != nil {
           return nil, err
       }

       stems := QueryStems(query)
       merged := mergeAndRank(titleHits, msgHits, 20.0, 10.0, limit, stems)
       return merged, nil
   }

   // mergeAndRank combines title + content hits into per-conversation rows,
   // scored by max(titleScore × titleWeight, contentScore × contentWeight),
   // builds snippet + highlight marks for the top-scoring content message.
   func mergeAndRank(
       titleHits []repository.ConversationTitleHit,
       msgHits []repository.MessageSearchHit,
       titleW, contentW float64,
       limit int,
       stems map[string]struct{},
   ) []SearchResult {
       byID := make(map[string]*SearchResult)
       for _, t := range titleHits {
           byID[t.ID] = &SearchResult{
               ConversationID: t.ID,
               Title:          t.Title,
               ProjectID:      t.ProjectID,
               Score:          t.Score * titleW,
               LastMessageAt:  t.LastMessageAt,
           }
       }
       for _, m := range msgHits {
           score := m.TopScore * contentW
           snippet := BuildSnippet(m.TopContent, stems)
           marks := HighlightRanges(snippet, stems)
           if existing, ok := byID[m.ConversationID]; ok {
               // Title and content both hit; keep stronger score, fill snippet from content.
               if score > existing.Score {
                   existing.Score = score
               }
               existing.Snippet = snippet
               existing.MatchCount = m.MatchCount
               existing.TopMessageID = m.TopMessageID
               existing.Marks = marks
           } else {
               byID[m.ConversationID] = &SearchResult{
                   ConversationID: m.ConversationID,
                   Score:          score,
                   Snippet:        snippet,
                   MatchCount:     m.MatchCount,
                   TopMessageID:   m.TopMessageID,
                   Marks:          marks,
               }
           }
       }
       out := make([]SearchResult, 0, len(byID))
       for _, v := range byID {
           out = append(out, *v)
       }
       sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
       if len(out) > limit {
           out = out[:limit]
       }
       return out
   }
   ```

3. Fill out the scaffold tests in `services/api/internal/service/search_test.go` and `snippet_test.go` with the concrete cases from RESEARCH §10 + the captureLogs pattern from titler_test.go:64–71. All behaviors 1–9 must pass.

   Critical test for SEARCH-07 (LOG-LEAK threat mitigation):
   ```go
   func TestSearcher_LogShape_NoQueryText(t *testing.T) {
       buf := captureLogs(t)
       s := NewSearcher(&fakeConvRepo{}, &fakeMsgRepo{})
       s.MarkIndexesReady()
       const literalQuery = "конфиденциальныйпоиск42"
       _, _ = s.Search(context.Background(), "biz-1", "user-1", literalQuery, nil, 20)
       require.Contains(t, buf.String(), "query_length")
       require.NotContains(t, buf.String(), literalQuery, "query text leaked into logs (T-19-LOG-LEAK)")
   }
   ```

4. Run:
   ```bash
   cd services/api && GOWORK=off go build ./...
   cd services/api && GOWORK=off go test -race ./internal/service/... -run "TestSearcher|TestBuildSnippet|TestHighlightRanges|TestQueryStems"
   ```

   Both must exit 0.
  </action>
  <verify>
    <automated>cd services/api && GOWORK=off go build ./... && GOWORK=off go test -race ./internal/service/... -run "TestSearcher|TestBuildSnippet|TestHighlightRanges|TestQueryStems"</automated>
  </verify>
  <acceptance_criteria>
    - File exists: `services/api/internal/service/search.go` containing `func (s *Searcher) Search(`
    - File exists: `services/api/internal/service/snippet.go` containing `func BuildSnippet(` AND `func HighlightRanges(` AND `func QueryStems(`
    - `grep -c "kljensen/snowball/russian" services/api/internal/service/snippet.go` >= 1
    - `grep -c "ErrInvalidScope" services/api/internal/service/search.go` >= 1
    - `grep -c "ErrSearchIndexNotReady" services/api/internal/service/search.go` >= 1
    - `grep -c "query_length" services/api/internal/service/search.go` >= 1 (SEARCH-07 metadata field)
    - `grep -c "businessID == \"\" || userID == \"\"" services/api/internal/service/search.go` >= 1 (T-19-CROSS-TENANT defense)
    - Search.go does NOT contain `"query"` as a slog field key (only `"query_length"`); verify via `! grep -E '"query"\s*,' services/api/internal/service/search.go` (loose form catches any `"query", X` slog field)
    - `cd services/api && GOWORK=off go test -race ./internal/service/... -run "TestSearcher|TestBuildSnippet|TestHighlightRanges|TestQueryStems"` exits 0
    - The dedicated log-leak test asserts `!bytes.Contains(buf, []byte(literalQuery))` and passes
  </acceptance_criteria>
  <done>Searcher orchestration + BuildSnippet + HighlightRanges + QueryStems implemented with snowball; T-19-CROSS-TENANT, T-19-INDEX-503, T-19-LOG-LEAK all guarded; tests GREEN.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 4 [BLOCKING — index-readiness wiring + cross-tenant integration test]: Handler + main.go wiring + router registration + cross-tenant integration test</name>
  <files>services/api/internal/handler/search.go, services/api/internal/router/router.go, services/api/cmd/main.go, test/integration/search_test.go</files>
  <read_first>
    - services/api/internal/handler/conversation.go lines 202–284 (auth + parse + repo call + writeJSON pattern)
    - services/api/internal/router/router.go lines 71–164 (protected r.Group block where /search route registers)
    - services/api/cmd/main.go lines 88–177 (existing index/backfill block + service wiring)
    - test/integration/authorization_test.go lines 13–194 (two-user pattern verbatim template)
    - .planning/phases/19-search-sidebar-redesign/19-RESEARCH.md §7 (lines 580–676 — atomic.Bool readiness flag + handler + main.go wiring)
    - .planning/phases/19-search-sidebar-redesign/19-PATTERNS.md §7 + §8 (handler + main.go specifics)
  </read_first>
  <behavior>
    - Test 1 (handler unit): GET /search?q=a (len < 2) → 400.
    - Test 2 (handler unit): GET /search WITHOUT bearer → 401.
    - Test 3 (handler unit): GET /search with `searcher.indexReady = false` → 503 + `Retry-After: 5` header (T-19-INDEX-503 mitigation).
    - Test 4 (handler unit): GET /search?q=инвойс success → 200 + JSON `[]SearchResult`. Captured logs contain `query_length` and not the literal `«инвойс»` (T-19-LOG-LEAK).
    - Test 5 (main.go startup): Build succeeds; `EnsureSearchIndexes` is called BEFORE `searcher.MarkIndexesReady()` (verify via grep ordering).
    - Test 6 (integration TestSearchCrossTenant): User A's `GET /search?q=инвойс` returns ONLY User A's conversation. User B's conversations NEVER appear in User A's response (T-19-CROSS-TENANT mitigation).
    - Test 7 (integration TestSearchProjectScope): With `?project_id=…`, only conversations in that project are returned.
    - Test 8 (integration TestSearchAggregatedShape): Response is `[]SearchResult` keyed by conversation ID, not raw messages.
  </behavior>
  <action>
1. Create `services/api/internal/handler/search.go` (NEW). Concrete shape (RESEARCH §7 + PATTERNS §7):
   ```go
   package handler

   import (
       "errors"
       "log/slog"
       "net/http"
       "strconv"
       "strings"

       "github.com/f1xgun/onevoice/pkg/domain"
       "github.com/f1xgun/onevoice/services/api/internal/middleware"
       "github.com/f1xgun/onevoice/services/api/internal/service"
   )

   type SearchHandler struct {
       searcher       *service.Searcher
       businessService BusinessLookup     // narrow interface — see services/api/internal/handler/interfaces.go pattern
   }

   type BusinessLookup interface {
       GetByUserID(ctx context.Context, userID uuid.UUID) (*domain.Business, error)
   }

   func NewSearchHandler(searcher *service.Searcher, biz BusinessLookup) (*SearchHandler, error) {
       if searcher == nil {
           return nil, fmt.Errorf("NewSearchHandler: searcher cannot be nil")
       }
       if biz == nil {
           return nil, fmt.Errorf("NewSearchHandler: biz lookup cannot be nil")
       }
       return &SearchHandler{searcher: searcher, businessService: biz}, nil
   }

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
       biz, err := h.businessService.GetByUserID(r.Context(), userID)
       if err != nil {
           writeJSONError(w, http.StatusUnauthorized, "no business")
           return
       }
       var projectID *string
       if p := strings.TrimSpace(r.URL.Query().Get("project_id")); p != "" {
           projectID = &p
       }
       limit := 20
       if l := r.URL.Query().Get("limit"); l != "" {
           if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 50 {
               limit = n
           }
       }

       results, err := h.searcher.Search(r.Context(), biz.ID.String(), userID.String(), q, projectID, limit)
       if errors.Is(err, domain.ErrSearchIndexNotReady) {
           // SEARCH-06 / T-19-INDEX-503 mitigation
           w.Header().Set("Retry-After", "5")
           writeJSONError(w, http.StatusServiceUnavailable, "search index initializing")
           return
       }
       if errors.Is(err, domain.ErrInvalidScope) {
           // T-19-CROSS-TENANT defense-in-depth — should never reach here because handler resolved scope server-side
           slog.ErrorContext(r.Context(), "search: invalid scope reached handler",
               "user_id", userID.String(), "business_id", biz.ID.String(), "query_length", len(q))
           writeJSONError(w, http.StatusInternalServerError, "scope error")
           return
       }
       if err != nil {
           // SEARCH-07 / T-19-LOG-LEAK — metadata only
           slog.ErrorContext(r.Context(), "search failed",
               "user_id", userID.String(),
               "business_id", biz.ID.String(),
               "query_length", len(q),
               "error", err)
           writeJSONError(w, http.StatusInternalServerError, "search failed")
           return
       }
       writeJSON(w, http.StatusOK, results)
   }
   ```

2. Edit `services/api/internal/router/router.go`. Inside the protected `r.Group` (around lines 71–164), add:
   ```go
   r.Get("/search", handlers.Search.Search)
   ```
   Add `Search *handler.SearchHandler` to the `Handlers` struct (lines 19–34).

3. Edit `services/api/cmd/main.go`. After the Phase 19 backfill block (added in 19-02 task 2) and AFTER the existing `EnsureConversationIndexes` call, add:
   ```go
   // Phase 19 — text indexes for sidebar search (SEARCH-01).
   indexesCtx3, indexesCancel3 := context.WithTimeout(ctx, 60*time.Second)
   if err := repository.EnsureSearchIndexes(indexesCtx3, mongoDB); err != nil {
       indexesCancel3()
       slog.ErrorContext(indexesCtx3, "failed to ensure search text indexes", "error", err)
       return fmt.Errorf("ensure search indexes: %w", err)
   }
   indexesCancel3()
   ```

   Then in the service-wiring section (around line 177 — find via grep `service.New`), add:
   ```go
   searcher := service.NewSearcher(conversationRepo, messageRepo)
   // CRITICAL: MarkIndexesReady MUST happen AFTER EnsureSearchIndexes returns nil.
   // The atomic.Bool's Store ensures a happens-before edge with every subsequent Load by handler goroutines.
   searcher.MarkIndexesReady()
   ```

   Then in the handler-wiring section (around line 365–399), construct the search handler and add it to `Handlers`:
   ```go
   searchHandler, err := handler.NewSearchHandler(searcher, businessService)
   if err != nil {
       return fmt.Errorf("new search handler: %w", err)
   }
   handlers.Search = searchHandler
   ```

   **BLOCKING ordering check**: the source file's structure MUST place `EnsureSearchIndexes` BEFORE `searcher.MarkIndexesReady()`. The directive's grep check is:
   ```bash
   grep -B2 -A20 "searchReady.Store\|searcher.MarkIndexesReady" services/api/cmd/main.go
   ```
   The output MUST show `EnsureSearchIndexes` appearing in the lines preceding `MarkIndexesReady`. If a future automated reorder reverses this, the build is correct but the contract is broken — readiness flag would flip before indexes exist.

4. Fill out `services/api/internal/handler/search_test.go` with concrete tests for behaviors 1–4. Use httptest.NewRecorder and a fake Searcher (or the real Searcher with a fake repo).

5. Fill out `test/integration/search_test.go` with concrete bodies for `TestSearchCrossTenant`, `TestSearchProjectScope`, `TestSearchAggregatedShape`, `TestSearchEmptyQueryReturns400`, `TestSearchMissingBearerReturns401`. Use the helper added in Task 1 (`createConversationWithMessage`).

6. Run:
   ```bash
   cd services/api && GOWORK=off go build ./...
   grep -B2 -A20 "searcher.MarkIndexesReady" services/api/cmd/main.go    # show ordering
   grep -q "EnsureSearchIndexes" services/api/cmd/main.go && grep -q "searcher.MarkIndexesReady" services/api/cmd/main.go   # both present
   cd services/api && GOWORK=off go test -race ./internal/handler/... -run "TestSearchHandler"
   cd test/integration && GOWORK=off go test -race -tags=integration -run "TestSearchCrossTenant|TestSearchProjectScope|TestSearchAggregatedShape|TestSearchEmptyQueryReturns400|TestSearchMissingBearerReturns401" ./...
   ```

   All must exit 0.
  </action>
  <verify>
    <automated>cd services/api && GOWORK=off go build ./... && grep -q "EnsureSearchIndexes" cmd/main.go && grep -q "searcher.MarkIndexesReady" cmd/main.go && python3 -c "import re,sys;src=open('cmd/main.go').read();ei=src.find('EnsureSearchIndexes');mi=src.find('MarkIndexesReady');sys.exit(0 if 0<ei<mi else 1)" && GOWORK=off go test -race ./internal/handler/... -run "TestSearchHandler" && cd /Users/f1xgun/onevoice/.worktrees/milestone-1.3/test/integration && GOWORK=off go test -race -tags=integration -run "TestSearchCrossTenant|TestSearchProjectScope|TestSearchAggregatedShape|TestSearchEmptyQueryReturns400|TestSearchMissingBearerReturns401" ./...</automated>
  </verify>
  <acceptance_criteria>
    - File exists: `services/api/internal/handler/search.go` containing `func (h *SearchHandler) Search(` AND `Retry-After`
    - `grep -c "/search" services/api/internal/router/router.go` >= 1
    - `grep -q "EnsureSearchIndexes" services/api/cmd/main.go` exits 0
    - `grep -q "searcher.MarkIndexesReady" services/api/cmd/main.go` exits 0
    - The byte offset of `EnsureSearchIndexes` in `services/api/cmd/main.go` is LESS than the byte offset of `MarkIndexesReady` (BLOCKING ordering check)
    - `cd services/api && GOWORK=off go build ./...` exits 0
    - `cd services/api && GOWORK=off go test -race ./internal/handler/... -run "TestSearchHandler"` exits 0
    - `cd test/integration && GOWORK=off go test -race -tags=integration -run "TestSearchCrossTenant"` exits 0 (BLOCKING — proves T-19-CROSS-TENANT contract)
    - `cd test/integration && GOWORK=off go test -race -tags=integration -run "TestSearchProjectScope|TestSearchAggregatedShape"` exits 0
  </acceptance_criteria>
  <done>SearchHandler + route + main.go wiring (with index-creation BEFORE readiness flip) + cross-tenant integration test landed; all three named threats mitigated and proven by tests.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| client → API | `GET /api/v1/search` with bearer JWT; user-id and business-id derived server-side. |
| API → MongoDB | Repository signature `(business_id, user_id, query, project_id?)` — empty values rejected with `ErrInvalidScope`. |
| API → process logs | slog metadata-only contract: `{user_id, business_id, query_length}`; query text NEVER logged. |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-19-CROSS-TENANT | Information Disclosure / Elevation of Privilege | `SearchService.Search` and `SearchTitles`/`ScopedConversationIDs`/`SearchByConversationIDs` repository methods | mitigate | (1) Repository signatures REQUIRE `(business_id, user_id)`; empty values return `ErrInvalidScope`. (2) Phase-1 `Find` filters `{user_id, business_id, project_id?}` — Mongo `$text` scoping rule honored (`$text` first `$match` + non-$text equality filters). (3) Phase-2 `$in: convIDs` allowlist — Message has no `business_id`, so cross-tenant scope is enforced by the conversation-id allowlist from Phase 1. (4) Cross-tenant integration test `test/integration/search_test.go::TestSearchCrossTenant` proves Business B's messages NEVER leak to Business A — BLOCKING acceptance. |
| T-19-INDEX-503 | Denial of Service / Availability | `GET /search` returning 500 because `EnsureSearchIndexes` hasn't completed at cold deploy | mitigate | (1) `searchReady atomic.Bool` flag; `Searcher.Search` returns `ErrSearchIndexNotReady` while flag is false. (2) Handler maps `ErrSearchIndexNotReady → 503 + Retry-After: 5`. (3) `main.go` wiring guarantees `EnsureSearchIndexes` returns nil BEFORE `searcher.MarkIndexesReady()` is called — happens-before edge enforced by `atomic.Bool.Store`. (4) Build-time grep ordering check in acceptance criteria. |
| T-19-LOG-LEAK | Information Disclosure | Search query text appearing in slog / Loki — query may contain PII or business-sensitive terms | mitigate | (1) Search service slog field shape locked to `{user_id, business_id, query_length}`. (2) NO `"query"` slog key anywhere in the search service or handler. (3) Dedicated unit test `TestSearcher_LogShape_NoQueryText` captures slog buffer with literal query and asserts `!bytes.Contains(buf, []byte(literalQuery))`. (4) Field-shape contract enforced by code review + grep audit. |

These three threats correspond directly to VALIDATION.md's `T-19-CROSS-TENANT`, `T-19-INDEX-503`, `T-19-LOG-LEAK` rows.
</threat_model>

<verification>
- `cd services/api && GOWORK=off go build ./...` succeeds
- Build-time ordering check: `EnsureSearchIndexes` offset < `MarkIndexesReady` offset in `services/api/cmd/main.go`
- `cd services/api && GOWORK=off go test -race ./internal/repository/... ./internal/service/... ./internal/handler/... -run "TestEnsureSearchIndexes|TestSearchTitles|TestScopedConversationIDs|TestSearchByConversationIDs|TestSearcher|TestBuildSnippet|TestHighlightRanges|TestQueryStems|TestSearchHandler"` exits 0
- `cd test/integration && GOWORK=off go test -race -tags=integration -run "TestSearchCrossTenant|TestSearchProjectScope|TestSearchAggregatedShape|TestSearchEmptyQueryReturns400|TestSearchMissingBearerReturns401" ./...` exits 0
- Manual: hitting `GET /api/v1/search?q=инвойс` with valid bearer on a populated dev DB returns aggregated rows scoped to the caller's business; returns 503 with `Retry-After: 5` if hit during cold boot before indexes finish
</verification>

<success_criteria>
- `github.com/kljensen/snowball v0.10.0` in `services/api/go.mod`
- `pkg/domain.ErrInvalidScope` and `pkg/domain.ErrSearchIndexNotReady` sentinels
- `EnsureSearchIndexes` creates two text indexes (title weight 20, content weight 10, default_language: russian) idempotently
- `SearchTitles` + `ScopedConversationIDs` + `SearchByConversationIDs` repo methods with defense-in-depth scope guards
- `Searcher` orchestration with two-phase + merge + rank + snippet + highlight ranges; ErrInvalidScope guard; atomic.Bool readiness flag
- `BuildSnippet` (±40-120, word-boundary, first-match-centered) + `HighlightRanges` + `QueryStems` pure helpers
- `GET /api/v1/search` handler with 400/401/503/200 mapping and metadata-only logging
- `EnsureSearchIndexes` wired into main.go startup BEFORE `searcher.MarkIndexesReady()` (BLOCKING ordering)
- Cross-tenant integration test in `test/integration/search_test.go` BLOCKING
- All three threats T-19-CROSS-TENANT, T-19-INDEX-503, T-19-LOG-LEAK mitigated and tested
- SEARCH-01..03, 05–07 all covered
</success_criteria>

<output>
After completion, create `.planning/phases/19-search-sidebar-redesign/19-03-SUMMARY.md` recording:
- The exact `BusinessLookup` interface name + the existing service it adapts (so 19-04 frontend types match)
- The exact JSON shape returned by `/search` (for OpenAPI / TypeScript types in 19-04)
- Any test corpus chosen for the snowball divergence pitfall (RESEARCH §1 «злейший»→«зл» note)
- Any deviations from RESEARCH §10's `BuildSnippet` algorithm (e.g., chosen `halfWindow` constant — RESEARCH says 50; planner may have used a different value)
- Confirmation that `EnsureSearchIndexes` is invoked BEFORE `MarkIndexesReady` in `cmd/main.go` (cite the line numbers)
</output>
