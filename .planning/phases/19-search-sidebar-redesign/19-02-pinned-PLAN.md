---
phase: 19-search-sidebar-redesign
plan: 02
type: execute
wave: 1
depends_on: []
files_modified:
  - pkg/domain/mongo_models.go
  - pkg/domain/mongo_models_test.go
  - services/api/internal/repository/conversation.go
  - services/api/internal/repository/mongo_backfill.go
  - services/api/internal/repository/mongo_backfill_test.go
  - services/api/internal/handler/conversation.go
  - services/api/internal/handler/conversation_test.go
  - services/api/internal/router/router.go
  - services/api/cmd/main.go
  - services/frontend/hooks/useConversations.ts
  - services/frontend/components/sidebar/PinnedSection.tsx
  - services/frontend/components/sidebar/ProjectSection.tsx
  - services/frontend/components/sidebar/UnassignedBucket.tsx
  - services/frontend/components/sidebar/ProjectPane.tsx
  - services/frontend/components/sidebar/__tests__/PinnedSection.test.tsx
  - services/frontend/components/chat/ChatHeader.tsx
  - services/frontend/components/chat/ProjectChip.tsx
autonomous: true
requirements:
  - UI-02
  - UI-03
threat_model_summary: "Atomic Mongo conditional update for pin/unpin scoped by (user_id, business_id); defense-in-depth scope filter prevents cross-tenant pin manipulation."
must_haves:
  truths:
    - "Russian UI copy is locked verbatim («Закреплённые», «Закрепить», «Открепить»)"
    - "pinned_at != nil is the SINGLE SOURCE OF TRUTH for the pinned state (D-02 — bool dropped via $unset in backfill)"
    - "BackfillConversationsV19 is wired into services/api/cmd/main.go startup sequence (not just defined)"
    - "Pin/Unpin repository methods scope by (user_id, business_id) — defense-in-depth (Pitfalls §19)"
    - "ChatHeader pin button uses NARROW MEMOIZED SELECTOR (Phase 18 D-11 pattern) — subscribes only to `pinned`, not whole conversation row"
    - "Empty pinned section is HIDDEN entirely (D-04 — no header, no placeholder)"
    - "Pinned chats render in BOTH PinnedSection AND under their own project (D-05 ProjectChip mini indicator on the global pinned row only)"
    - "Pin mutation invalidates ['conversations'] React Query cache — extends Phase 18 D-10 pattern"
    - "Compound index {user_id, business_id, project_id, pinned_at:-1, last_message_at:-1} is created idempotently (NEW index, does NOT extend the Phase 18 compound index)"
    - "All Go modules use `replace github.com/f1xgun/onevoice/pkg => ../../pkg`"
    - "t.Setenv not os.Setenv in tests"
  artifacts:
    - path: pkg/domain/mongo_models.go
      provides: "Conversation.PinnedAt *time.Time field; Conversation.Pinned bool removed"
      contains: "PinnedAt"
    - path: services/api/internal/repository/mongo_backfill.go
      provides: "BackfillConversationsV19 idempotent migration writing pinned_at, migrating legacy pinned bool, $unset legacy field"
      contains: "BackfillConversationsV19"
    - path: services/api/internal/repository/conversation.go
      provides: "Pin / Unpin atomic methods + new compound index in EnsureConversationIndexes"
      contains: "func (r *conversationRepository) Pin("
    - path: services/api/internal/handler/conversation.go
      provides: "POST /conversations/{id}/pin and POST /conversations/{id}/unpin handlers"
    - path: services/frontend/components/sidebar/PinnedSection.tsx
      provides: "Sibling of UnassignedBucket; renders pinned conversations with mini ProjectChip; hidden when empty"
      min_lines: 30
    - path: services/frontend/components/chat/ChatHeader.tsx
      provides: "Bookmark pin button with narrow memoized useConversationPinned selector"
      contains: "useConversationPinned"
    - path: services/frontend/components/chat/ProjectChip.tsx
      provides: "size?: 'xs' | 'sm' | 'md' prop with sizeClasses Record"
      contains: "sizeClasses"
  key_links:
    - from: services/api/cmd/main.go
      to: services/api/internal/repository/mongo_backfill.go
      via: BackfillConversationsV19(backfillCtx2, mongoDB)
      pattern: "BackfillConversationsV19"
    - from: services/api/internal/repository/conversation.go
      to: mongo conversations collection
      via: UpdateOne with filter {_id, business_id, user_id} and $set {pinned_at, updated_at}
      pattern: "pinned_at"
    - from: services/api/internal/router/router.go
      to: services/api/internal/handler/conversation.go
      via: r.Post(/conversations/{id}/pin, h.Pin) and /unpin
      pattern: "/pin"
    - from: services/frontend/components/sidebar/ProjectSection.tsx
      to: services/frontend/hooks/useConversations.ts
      via: usePinConversation mutation invalidating ['conversations']
      pattern: "invalidateQueries.*conversations"
---

<objective>
Add `PinnedAt *time.Time` to `Conversation`, drop legacy `Pinned bool` (D-02 single source of truth), idempotent backfill `BackfillConversationsV19` with three steps + schema_migrations marker, NEW compound index `{user_id, business_id, project_id, pinned_at:-1, last_message_at:-1}`. Add atomic `Pin(ctx, id, businessID, userID)` / `Unpin(...)` repo methods scoped by `(business_id, user_id)`. Expose `POST /api/v1/conversations/{id}/pin` + `/unpin` handlers and route them. Frontend: extend `useConversations.ts` with `usePinConversation` mutation invalidating `['conversations']`; new `<PinnedSection>` component (sibling of UnassignedBucket, hidden when empty); extend `ProjectSection` and `UnassignedBucket` with «Закрепить»/«Открепить» context-menu items + bookmark indicator on pinned rows; add bookmark button to `ChatHeader` using narrow memoized `useConversationPinned` selector (Phase 18 D-11 mitigation pattern); extend `ProjectChip` with `size?: 'xs'|'sm'|'md'` prop and use `size="xs"` in PinnedSection rows for project affiliation indicator (D-05).

Purpose: UI-02 (sidebar projects + Без проекта + Закреплённые) and UI-03 (pin/unpin context menu + global pinned + duplication under project).

Output: schema migration + atomic repo methods + endpoints + sidebar pinned section + chat header pin button.
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
@.planning/phases/15-projects-foundation/15-CONTEXT.md
@.planning/phases/18-auto-title/18-CONTEXT.md
@services/api/AGENTS.md
@services/frontend/AGENTS.md
@pkg/AGENTS.md
@docs/api-design.md
@docs/security.md
@docs/go-style.md

<interfaces>
<!-- Existing pkg/domain.Conversation (mongo_models.go:39-50) — Phase 19 modifies in place -->

```go
type Conversation struct {
    ID            string     `json:"id" bson:"_id,omitempty"`
    UserID        string     `json:"userId" bson:"user_id"`
    BusinessID    string     `json:"businessId" bson:"business_id"`
    ProjectID     *string    `json:"projectId,omitempty" bson:"project_id"`
    Title         string     `json:"title" bson:"title"`
    TitleStatus   string     `json:"titleStatus" bson:"title_status"`
    Pinned        bool       `json:"pinned" bson:"pinned"`              // REMOVED in Phase 19 D-02
    PinnedAt      *time.Time `json:"pinnedAt,omitempty" bson:"pinned_at,omitempty"`  // NEW
    LastMessageAt *time.Time `json:"lastMessageAt,omitempty" bson:"last_message_at,omitempty"`
    CreatedAt     time.Time  `json:"createdAt" bson:"created_at"`
    UpdatedAt     time.Time  `json:"updatedAt" bson:"updated_at"`
}
```

<!-- Existing services/api/internal/repository/conversation.go signature seam (callable contracts): -->
- `func (r *conversationRepository) UpdateTitleIfPending(ctx, id, title string) error` — atomic conditional pattern at lines 155–177
- `func EnsureConversationIndexes(ctx context.Context, db *mongo.Database) error` at lines 224–243 (extend with new compound index)
- `func (r *conversationRepository) GetByID(ctx, id) (*domain.Conversation, error)` — used by handler for ownership recheck pattern

<!-- Existing services/api/internal/repository/mongo_backfill.go shape: -->
- `const SchemaMigrationPhase15 = "phase-15-projects-foundation"`
- `func BackfillConversationsV15(ctx, db) error` — fast-path marker check, per-field guarded $set, marker upsert
- `func backfillField(ctx, coll, fieldName string, filter, update bson.M) error` — helper at lines 104–112

<!-- Existing services/api/internal/handler/conversation.go patterns: -->
- `middleware.GetUserID(r.Context()) → uuid.UUID, error` (NOT UserIDFromContext — verified PATTERNS §7 line 446)
- `writeJSON`, `writeJSONError` package-level helpers
- Existing handler factory: `NewConversationHandler(repo, ...) (*ConversationHandler, error)`

<!-- Existing services/api/internal/router/router.go Handlers struct (lines 19–34) needs no new field for pin/unpin (added to ConversationHandler — same handler struct gets two more methods). -->

<!-- Frontend useConversations.ts already exposes useConversations(), useCreateConversation(), useDeleteConversation(), useUpdateConversation(). Extend with usePinConversation, useUnpinConversation mutations. -->

<!-- ChatHeader.tsx narrow-selector pattern (Phase 18 D-11 — to copy verbatim): -->
```tsx
function useConversationTitle(conversationId: string): string {
    const { data } = useQuery<Conversation[], Error, string>({
        queryKey: ['conversations'],
        queryFn: () => api.get('/conversations').then((r) => r.data),
        select: (list) => { /* projection to primitive */ },
        enabled: !!conversationId,
    });
    return data ?? '';
}
```
</interfaces>
</context>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: Domain field swap + repo Pin/Unpin atomic methods + new compound index</name>
  <files>pkg/domain/mongo_models.go, pkg/domain/mongo_models_test.go, services/api/internal/repository/conversation.go, services/api/internal/repository/conversation_test.go</files>
  <read_first>
    - pkg/domain/mongo_models.go lines 39–50 (existing Conversation struct shape)
    - services/api/internal/repository/conversation.go lines 155–177 (UpdateTitleIfPending — atomic conditional analog) AND lines 224–243 (EnsureConversationIndexes — index extension target)
    - .planning/phases/19-search-sidebar-redesign/19-RESEARCH.md §11 (lines 992–1058 — ESR analysis for new compound index; do NOT extend Phase 18's index)
    - .planning/phases/19-search-sidebar-redesign/19-RESEARCH.md §12 (lines 1062–1167 — domain change rationale)
    - .planning/phases/19-search-sidebar-redesign/19-PATTERNS.md §1 + §2 (Pin/Unpin shape + index extension; Shared Patterns §1 atomic conditional update)
    - services/api/internal/repository/conversation_test.go (existing test patterns; verify there is a Pin/Unpin test file or extend this one)
  </read_first>
  <behavior>
    - Test 1 (domain): `Conversation{PinnedAt: &t}` JSON-marshals to `{"pinnedAt":"<iso>"}`; `Conversation{}` (zero) marshals WITHOUT a `pinnedAt` key (omitempty).
    - Test 2 (domain): BSON round-trip preserves `*time.Time`.
    - Test 3 (repo Pin): Pin sets `pinned_at` to now (UTC), updates `updated_at`, returns nil; subsequent `GetByID` shows non-nil `PinnedAt`.
    - Test 4 (repo Pin): Pin with mismatched `userID` returns `domain.ErrConversationNotFound` (defense-in-depth scope filter).
    - Test 5 (repo Pin): Pin with mismatched `businessID` returns `domain.ErrConversationNotFound`.
    - Test 6 (repo Unpin): Unpin sets `pinned_at` to nil; subsequent `GetByID` shows nil `PinnedAt`.
    - Test 7 (repo index): `EnsureConversationIndexes` called twice in succession — second call returns nil (idempotency); listIndexes shows `conversations_user_biz_proj_pinned_recency` present.
  </behavior>
  <action>
1. Edit `pkg/domain/mongo_models.go` lines 39–50: REMOVE the `Pinned bool` line and ADD `PinnedAt *time.Time \`json:"pinnedAt,omitempty" bson:"pinned_at,omitempty"\`` immediately above `LastMessageAt` (preserves logical grouping). Add a comment line above explaining: `// Pinned bool removed Phase 19 D-02 — single source of truth is PinnedAt != nil. See repository/mongo_backfill.go:BackfillConversationsV19 for migration.`

2. Search the repo for any code reading `conv.Pinned` (likely sites: `services/api/internal/repository/conversation.go`, `services/api/internal/handler/chat_proxy.go`, `services/api/internal/handler/conversation.go`, frontend types). For each Go reference: replace `conv.Pinned` with `conv.PinnedAt != nil`. For frontend type generation (if any): update `Conversation` type to drop `pinned: boolean` and add `pinnedAt: string | null`.

3. Edit `services/api/internal/repository/conversation.go` — append two new methods to the `conversationRepository` value receiver. Concrete code (atomic conditional update analog of UpdateTitleIfPending lines 155–177):
   ```go
   // Pin sets pinned_at = now (UTC) on the conversation, scoped by (id, business_id, user_id)
   // for defense-in-depth (Pitfalls §19). Returns ErrConversationNotFound on mismatch.
   func (r *conversationRepository) Pin(ctx context.Context, id, businessID, userID string) error {
       now := time.Now().UTC()
       filter := bson.M{"_id": id, "business_id": businessID, "user_id": userID}
       update := bson.M{"$set": bson.M{"pinned_at": now, "updated_at": now}}
       res, err := r.collection.UpdateOne(ctx, filter, update)
       if err != nil {
           return fmt.Errorf("pin conversation: %w", err)
       }
       if res.MatchedCount == 0 {
           return domain.ErrConversationNotFound
       }
       return nil
   }

   // Unpin sets pinned_at = nil on the conversation, scoped by (id, business_id, user_id).
   // Returns ErrConversationNotFound on mismatch.
   func (r *conversationRepository) Unpin(ctx context.Context, id, businessID, userID string) error {
       now := time.Now().UTC()
       filter := bson.M{"_id": id, "business_id": businessID, "user_id": userID}
       update := bson.M{"$set": bson.M{"pinned_at": nil, "updated_at": now}}
       res, err := r.collection.UpdateOne(ctx, filter, update)
       if err != nil {
           return fmt.Errorf("unpin conversation: %w", err)
       }
       if res.MatchedCount == 0 {
           return domain.ErrConversationNotFound
       }
       return nil
   }
   ```

4. Add the `Pin` and `Unpin` method signatures to the `ConversationRepository` interface in `pkg/domain` (find the interface — likely `pkg/domain/repository_interfaces.go` or inline in `mongo_models.go`/dedicated file; grep for `type ConversationRepository interface`). Add:
   ```go
   Pin(ctx context.Context, id, businessID, userID string) error
   Unpin(ctx context.Context, id, businessID, userID string) error
   ```

5. Edit `services/api/internal/repository/conversation.go` `EnsureConversationIndexes` (lines 224–243). Append a SECOND `mongo.IndexModel` entry to the existing `models := []mongo.IndexModel{...}` slice. Concrete value:
   ```go
   {
       Keys: bson.D{
           {Key: "user_id", Value: 1},
           {Key: "business_id", Value: 1},
           {Key: "project_id", Value: 1},
           {Key: "pinned_at", Value: -1},
           {Key: "last_message_at", Value: -1},
       },
       Options: options.Index().SetName("conversations_user_biz_proj_pinned_recency"),
   },
   ```
   Do NOT touch the existing Phase 18 index (`conversations_user_biz_title_status`) — D-08a is locked.

6. Add unit tests to `services/api/internal/repository/conversation_test.go` (or a new `pin_test.go` sibling if the file is too crowded). Use the existing test mongo setup helper (find via grep `setupTestMongo` or similar in the repository). Tests cover behaviors 3–7.

7. Add JSON marshaling tests to `pkg/domain/mongo_models_test.go` covering behaviors 1–2.

8. Run:
   ```bash
   cd services/api && GOWORK=off go test -race ./internal/repository/... -run "TestPin|TestUnpin|TestEnsureConversationIndexes"
   cd /Users/f1xgun/onevoice/.worktrees/milestone-1.3 && GOWORK=off go test -race ./pkg/domain/... -run TestConversation
   ```

   Both must exit 0.
  </action>
  <verify>
    <automated>cd services/api && GOWORK=off go build ./... && GOWORK=off go test -race ./internal/repository/... -run "TestPin|TestUnpin|TestEnsureConversationIndexes" && cd /Users/f1xgun/onevoice/.worktrees/milestone-1.3 && GOWORK=off go test -race ./pkg/domain/... -run TestConversation</automated>
  </verify>
  <acceptance_criteria>
    - `pkg/domain/mongo_models.go` contains `PinnedAt *time.Time` AND does NOT contain `Pinned bool`
    - `grep -c "conv.Pinned " services/api/` returns 0 (all callers migrated)
    - `services/api/internal/repository/conversation.go` contains both `func (r *conversationRepository) Pin(` and `func (r *conversationRepository) Unpin(`
    - `grep -c "conversations_user_biz_proj_pinned_recency" services/api/internal/repository/conversation.go` >= 1
    - `grep -c "conversations_user_biz_title_status" services/api/internal/repository/conversation.go` >= 1 (Phase 18 index untouched)
    - `cd services/api && GOWORK=off go build ./...` exits 0
    - `cd services/api && GOWORK=off go test -race ./internal/repository/... -run "TestPin|TestUnpin|TestEnsureConversationIndexes"` exits 0
    - `cd /Users/f1xgun/onevoice/.worktrees/milestone-1.3 && GOWORK=off go test -race ./pkg/domain/... -run TestConversation` exits 0
  </acceptance_criteria>
  <done>Domain field swapped; Pin/Unpin atomic methods scoped by (id, business_id, user_id); new compound index added without disturbing Phase 18 index; tests GREEN.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2 [BLOCKING — backfill wired into startup]: BackfillConversationsV19 idempotent migration + main.go wiring + Pin/Unpin handlers + route registration</name>
  <files>services/api/internal/repository/mongo_backfill.go, services/api/internal/repository/mongo_backfill_test.go, services/api/internal/handler/conversation.go, services/api/internal/handler/conversation_test.go, services/api/internal/router/router.go, services/api/cmd/main.go</files>
  <read_first>
    - services/api/internal/repository/mongo_backfill.go lines 35–112 (BackfillConversationsV15 + backfillField helper — VERBATIM template)
    - services/api/internal/repository/mongo_backfill_test.go (existing test patterns for idempotency assertions)
    - services/api/cmd/main.go lines 88–130 (Phase 15 backfill block + Phase 18 indexes block — concrete patterns to copy)
    - services/api/internal/handler/conversation.go lines 202–284 (existing GetConversation/ListConversations patterns; auth + parse + repo call + writeJSON)
    - services/api/internal/router/router.go lines 71–164 (existing protected r.Group block where pin/unpin routes register)
    - .planning/phases/19-search-sidebar-redesign/19-RESEARCH.md §12 (lines 1062–1140 — V19 backfill body in 3 steps; lines 1169–1180 — main.go wiring)
    - .planning/phases/19-search-sidebar-redesign/19-PATTERNS.md §4 + §7 + §8 (backfill + handler + main.go wiring)
  </read_first>
  <behavior>
    - Test 1 (backfill): Empty conversations collection — BackfillConversationsV19 succeeds, writes marker, no document writes. Second call is a no-op (marker fast-path).
    - Test 2 (backfill step 1): Conversations missing `pinned_at` field get `pinned_at: null`. Idempotent — re-run does not create extra writes (marker fast-path skips).
    - Test 3 (backfill step 2): Legacy `pinned: true` documents with `pinned_at: nil` get `pinned_at = updated_at`.
    - Test 4 (backfill step 3): Legacy `pinned` field is dropped via `$unset`. After backfill, no document has a `pinned` field.
    - Test 5 (backfill marker): `schema_migrations` collection contains `_id: "phase-19-search-sidebar-pinned-at"` after run.
    - Test 6 (handler Pin): `POST /api/v1/conversations/{id}/pin` with valid bearer returns 200 + updated conversation; conversation in DB has non-nil `pinned_at`.
    - Test 7 (handler Pin): `POST /api/v1/conversations/{id}/pin` with bearer of different user returns 404 (ErrConversationNotFound mapped). NEVER 403 — leaking existence-vs-ownership distinction is a known anti-pattern; the scope-filtered repo MatchedCount==0 is the correct uniform 404.
    - Test 8 (handler Unpin): `POST /api/v1/conversations/{id}/unpin` with valid bearer returns 200; conversation has nil `pinned_at`.
    - Test 9 (handler): Missing bearer returns 401.
    - Test 10 (handler): Invalid `id` (non-24-char Mongo ObjectID) returns 400 — same shape as existing GetConversation guard.
    - Test 11 (main.go startup wiring): Build succeeds; `BackfillConversationsV19` invocation is grep-discoverable in main.go.
  </behavior>
  <action>
1. Edit `services/api/internal/repository/mongo_backfill.go`. Add the constant and function from RESEARCH §12 lines 1086–1140 verbatim, with corrections to use existing `backfillField` helper (lines 104–112) where possible:
   ```go
   const SchemaMigrationPhase19 = "phase-19-search-sidebar-pinned-at"

   func BackfillConversationsV19(ctx context.Context, db *mongo.Database) error {
       conversations := db.Collection("conversations")
       marker := db.Collection("schema_migrations")

       // Fast-path: if marker exists, skip — idempotent on restart.
       var existing bson.M
       err := marker.FindOne(ctx, bson.M{"_id": SchemaMigrationPhase19}).Decode(&existing)
       if err == nil {
           slog.InfoContext(ctx, "phase 19 backfill already applied", "marker", SchemaMigrationPhase19)
           return nil
       }
       if !errors.Is(err, mongo.ErrNoDocuments) {
           return fmt.Errorf("read schema_migrations marker: %w", err)
       }

       // Step 1: pinned_at = null when missing.
       if err := backfillField(ctx, conversations, "pinned_at",
           bson.M{"pinned_at": bson.M{"$exists": false}},
           bson.M{"$set": bson.M{"pinned_at": nil}}); err != nil {
           return err
       }

       // Step 2: migrate legacy pinned:true → pinned_at = updated_at.
       legacyFilter := bson.M{"pinned": true, "pinned_at": nil}
       legacyUpdate := mongo.Pipeline{
           {{Key: "$set", Value: bson.D{{Key: "pinned_at", Value: "$updated_at"}}}},
       }
       if _, err := conversations.UpdateMany(ctx, legacyFilter, legacyUpdate); err != nil {
           return fmt.Errorf("migrate legacy pinned bool: %w", err)
       }

       // Step 3: drop the legacy pinned field. Single source of truth becomes PinnedAt != nil.
       if _, err := conversations.UpdateMany(ctx,
           bson.M{"pinned": bson.M{"$exists": true}},
           bson.M{"$unset": bson.M{"pinned": ""}}); err != nil {
           return fmt.Errorf("drop legacy pinned bool: %w", err)
       }

       // Marker (one-shot upsert).
       _, err = marker.UpdateOne(ctx,
           bson.M{"_id": SchemaMigrationPhase19},
           bson.M{"$set": bson.M{
               "_id":        SchemaMigrationPhase19,
               "applied_at": time.Now().UTC(),
           }},
           options.UpdateOne().SetUpsert(true),
       )
       if err != nil {
           return fmt.Errorf("write schema_migrations marker: %w", err)
       }
       slog.InfoContext(ctx, "phase 19 backfill complete", "marker", SchemaMigrationPhase19)
       return nil
   }
   ```

2. Edit `services/api/cmd/main.go`. Find the existing Phase 15 backfill block (lines 88–130). Immediately AFTER the existing `EnsureConversationIndexes` call (line ~127), and BEFORE the rest of service wiring, add the V19 backfill:
   ```go
   // Phase 19 — pinned_at backfill + drop legacy `pinned` bool. Same shape as the Phase 15 backfill.
   backfillCtx2, backfillCancel2 := context.WithTimeout(ctx, 30*time.Second)
   if err := repository.BackfillConversationsV19(backfillCtx2, mongoDB); err != nil {
       backfillCancel2()
       slog.ErrorContext(backfillCtx2, "phase 19 backfill failed", "error", err)
       return fmt.Errorf("phase 19 backfill: %w", err)
   }
   backfillCancel2()
   ```

   Verify that the `EnsureConversationIndexes` call (which now creates the new compound index from Task 1) is invoked BEFORE this backfill. Order: index creation → backfill → service wiring. (Indexes don't depend on data shape; backfill should happen regardless of indexes; either order works in practice but the existing code calls indexes after backfill — preserve that order if the file currently does, then add V19 backfill right after V15 backfill, before EnsureConversationIndexes — but since EnsureConversationIndexes lookups don't depend on `pinned_at`, either placement is correct. Concrete choice: place V19 backfill IMMEDIATELY AFTER V15 backfill to match the same backfill-first pattern.)

3. Edit `services/api/internal/handler/conversation.go`. Append two new methods:
   ```go
   // Pin handles POST /api/v1/conversations/{id}/pin.
   func (h *ConversationHandler) Pin(w http.ResponseWriter, r *http.Request) {
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
       // Resolve businessID via the existing pattern (find via grep — likely h.businessSvc.GetByUserID or already on the request context).
       biz, err := h.businessService.GetByUserID(r.Context(), userID)
       if err != nil {
           writeJSONError(w, http.StatusUnauthorized, "no business")
           return
       }
       if err := h.conversationRepo.Pin(r.Context(), conversationID, biz.ID.String(), userID.String()); err != nil {
           if errors.Is(err, domain.ErrConversationNotFound) {
               writeJSONError(w, http.StatusNotFound, "conversation not found")
               return
           }
           slog.ErrorContext(r.Context(), "pin conversation failed",
               "conversation_id", conversationID,
               "user_id", userID.String(),
               "business_id", biz.ID.String(),
               "error", err)
           writeJSONError(w, http.StatusInternalServerError, "internal server error")
           return
       }
       // Return refreshed conversation
       conv, err := h.conversationRepo.GetByID(r.Context(), conversationID)
       if err != nil {
           writeJSONError(w, http.StatusInternalServerError, "internal server error")
           return
       }
       writeJSON(w, http.StatusOK, conv)
   }

   // Unpin handles POST /api/v1/conversations/{id}/unpin.
   // (Symmetric — same shape calling h.conversationRepo.Unpin.)
   func (h *ConversationHandler) Unpin(w http.ResponseWriter, r *http.Request) { /* ... */ }
   ```

   Match the EXACT field name on `ConversationHandler` for the business service — search the existing constructor (`NewConversationHandler`) to learn whether it's `businessService`, `businessSvc`, or accessed via another path. Use what's already there.

4. Edit `services/api/internal/router/router.go`. Inside the protected `r.Group(...)` block (around lines 71–164), find the existing `/conversations/{id}` route group and add:
   ```go
   r.Post("/conversations/{id}/pin", handlers.Conversation.Pin)
   r.Post("/conversations/{id}/unpin", handlers.Conversation.Unpin)
   ```
   Place them adjacent to existing `/conversations/{id}/move` route for consistency.

5. Add tests:
   - `services/api/internal/repository/mongo_backfill_test.go`: behaviors 1–5. Use the existing test mongo setup pattern from `mongo_backfill_test.go`.
   - `services/api/internal/handler/conversation_test.go`: behaviors 6–10. Mirror the existing `TestGetConversation` shape.

6. Run:
   ```bash
   cd services/api && GOWORK=off go build ./...
   cd services/api && GOWORK=off go test -race ./internal/repository/... -run "TestBackfillConversationsV19"
   cd services/api && GOWORK=off go test -race ./internal/handler/... -run "TestConversation_Pin|TestConversation_Unpin"
   ```

   All must exit 0.

7. Behavior 11 verification (BLOCKING per directive):
   ```bash
   grep -q "BackfillConversationsV19" services/api/cmd/main.go && cd services/api && GOWORK=off go build ./...
   ```
   Exit code 0 only when both: (a) the function name appears in main.go, (b) the entire `services/api` package builds.
  </action>
  <verify>
    <automated>cd services/api && GOWORK=off go build ./... && grep -q "BackfillConversationsV19" cmd/main.go && GOWORK=off go test -race ./internal/repository/... -run "TestBackfillConversationsV19" && GOWORK=off go test -race ./internal/handler/... -run "TestConversation_Pin|TestConversation_Unpin"</automated>
  </verify>
  <acceptance_criteria>
    - `services/api/internal/repository/mongo_backfill.go` contains `const SchemaMigrationPhase19 = "phase-19-search-sidebar-pinned-at"`
    - `services/api/internal/repository/mongo_backfill.go` contains `func BackfillConversationsV19(`
    - `grep -q "BackfillConversationsV19" services/api/cmd/main.go` — exit 0 (BLOCKING wiring check)
    - `services/api/internal/handler/conversation.go` contains `func (h *ConversationHandler) Pin(` AND `func (h *ConversationHandler) Unpin(`
    - `grep -c "/pin" services/api/internal/router/router.go` >= 1 AND `grep -c "/unpin" services/api/internal/router/router.go` >= 1
    - `cd services/api && GOWORK=off go build ./...` exits 0
    - `cd services/api && GOWORK=off go test -race ./internal/repository/... -run "TestBackfillConversationsV19"` exits 0
    - `cd services/api && GOWORK=off go test -race ./internal/handler/... -run "TestConversation_Pin|TestConversation_Unpin"` exits 0
  </acceptance_criteria>
  <done>Idempotent V19 backfill wired into startup; Pin/Unpin handlers + routes registered; tests GREEN; build succeeds.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 3: Frontend pin mutations + PinnedSection + ProjectChip size + ChatHeader bookmark + sidebar context-menu items</name>
  <files>services/frontend/hooks/useConversations.ts, services/frontend/components/sidebar/PinnedSection.tsx, services/frontend/components/sidebar/ProjectSection.tsx, services/frontend/components/sidebar/UnassignedBucket.tsx, services/frontend/components/sidebar/ProjectPane.tsx, services/frontend/components/sidebar/__tests__/PinnedSection.test.tsx, services/frontend/components/chat/ChatHeader.tsx, services/frontend/components/chat/ProjectChip.tsx, services/frontend/components/chat/__tests__/ChatHeader.isolation.test.tsx</files>
  <read_first>
    - services/frontend/hooks/useConversations.ts (existing useConversations + mutations — extend with usePinConversation/useUnpinConversation)
    - services/frontend/components/sidebar/UnassignedBucket.tsx (entire file — analog for PinnedSection)
    - services/frontend/components/sidebar/ProjectSection.tsx (entire file — context-menu pattern + place to insert pin item)
    - services/frontend/components/chat/ChatHeader.tsx lines 32–58 (existing useConversationTitle narrow selector — D-11 mitigation; PARALLEL pattern for useConversationPinned)
    - services/frontend/components/chat/ProjectChip.tsx lines 14–43 (chipBase + render — extend with size variants)
    - services/frontend/components/chat/MoveChatMenuItem.tsx lines 70–108 (Radix DropdownMenu sub-menu pattern; reference for new pin item)
    - .planning/phases/19-search-sidebar-redesign/19-PATTERNS.md §18 (PinnedSection), §21–§22 (ProjectSection / UnassignedBucket extensions), §23 (ChatHeader), §24 (ProjectChip size prop)
    - .planning/phases/19-search-sidebar-redesign/19-CONTEXT.md D-01, D-03, D-04, D-05 (UX rules — locked verbatim)
    - .planning/phases/18-auto-title/18-CONTEXT.md D-11 (USER OVERRIDE — narrow memo selector pattern, locked)
  </read_first>
  <behavior>
    - Test 1 (hook): `usePinConversation()` returns a mutation that, on success, calls `queryClient.invalidateQueries({ queryKey: ['conversations'] })`.
    - Test 2 (hook): `usePinConversation` calls `api.post('/conversations/{id}/pin')`.
    - Test 3 (hook): `useUnpinConversation` calls `api.post('/conversations/{id}/unpin')`.
    - Test 4 (PinnedSection): Render with `conversations: []` → component returns null (D-04 hidden when empty).
    - Test 5 (PinnedSection): Render with one pinned conversation → header «Закреплённые» renders + chat-row Link renders + ProjectChip with `size="xs"` renders for chats with `projectId != null`.
    - Test 6 (PinnedSection): Chat with `projectId: null` does NOT render a ProjectChip (D-05 — Без проекта chats get NO chip in pinned row).
    - Test 7 (PinnedSection): Chats sorted by `pinnedAt desc` (most recent first).
    - Test 8 (ProjectSection extension): Right-click (or trigger) on chat row opens DropdownMenu; menu contains either «Закрепить» (when `pinnedAt == null`) or «Открепить» (when `pinnedAt != null`) at the top.
    - Test 9 (ProjectSection extension): Pinned chat row renders a small `<Bookmark size={10} className="text-yellow-400" />` icon next to the title.
    - Test 10 (UnassignedBucket extension): Same context-menu item + indicator behavior as ProjectSection.
    - Test 11 (ChatHeader): With pinned conversation in cache, bookmark button has `aria-label="Открепить чат"` AND icon class `fill-yellow-400`. With unpinned, `aria-label="Закрепить чат"` and grey class.
    - Test 12 (ChatHeader narrow selector): When the cache mutates UNRELATED fields of OTHER conversations (e.g., title of a different chat updates), the bookmark button does NOT re-render. Use a render-counter spy ref to assert (Phase 18 D-11 isolation pattern — reference the existing `ChatHeader.isolation` test if it exists).
    - Test 13 (ProjectChip size): ProjectChip with `size="xs"` renders the `xs` Tailwind classes. Default (no prop) renders `sm` classes.
  </behavior>
  <action>
1. Edit `services/frontend/hooks/useConversations.ts`. Append two new mutation hooks. Concrete code (mirroring the existing `useUpdateConversation` shape — find the export pattern first):
   ```ts
   export function usePinConversation() {
     const queryClient = useQueryClient();
     return useMutation({
       mutationFn: (conversationId: string) =>
         api.post(`/conversations/${conversationId}/pin`).then((r) => r.data),
       onSuccess: () => {
         queryClient.invalidateQueries({ queryKey: ['conversations'] });
       },
     });
   }

   export function useUnpinConversation() {
     const queryClient = useQueryClient();
     return useMutation({
       mutationFn: (conversationId: string) =>
         api.post(`/conversations/${conversationId}/unpin`).then((r) => r.data),
       onSuccess: () => {
         queryClient.invalidateQueries({ queryKey: ['conversations'] });
       },
     });
   }
   ```

2. Edit `services/frontend/components/chat/ProjectChip.tsx`. Add the `size` prop (PATTERNS §24):
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
     // existing JSX — replace the `chipBase` Tailwind string with `cn(chipBase, sizeClasses[size])`
   }
   ```

   Verify all existing call sites (sidebar.tsx, ChatHeader.tsx, etc.) still type-check (no size = `'sm'` default, no visual change).

3. Create `services/frontend/components/sidebar/PinnedSection.tsx` with `'use client'` directive. Mirror `UnassignedBucket.tsx` shape:
   ```tsx
   'use client';
   import { useState } from 'react';
   import Link from 'next/link';
   import { Bookmark, ChevronDown, ChevronRight } from 'lucide-react';
   import { cn } from '@/lib/utils';
   import { ProjectChip } from '@/components/chat/ProjectChip';
   import type { Conversation } from '@/types/conversation';

   interface Props {
     conversations: Conversation[];     // expected pre-sorted by pinnedAt desc by caller
     projectsById: Record<string, { id: string; name: string }>;
     activeConversationId?: string;
     onNavigate?: () => void;
   }

   export function PinnedSection({ conversations, projectsById, activeConversationId, onNavigate }: Props) {
     const [collapsed, setCollapsed] = useState(false);
     if (conversations.length === 0) return null;     // D-04 hidden when empty
     return (
       <div className="group/pinned">
         <button
           onClick={() => setCollapsed(v => !v)}
           aria-expanded={!collapsed}
           className="flex w-full items-center gap-1 rounded-md px-2 py-1.5 text-sm text-gray-300 hover:bg-gray-800"
         >
           {collapsed ? <ChevronRight size={12} /> : <ChevronDown size={12} />}
           <Bookmark size={12} className="text-yellow-400" />
           <span className="flex-1 truncate">Закреплённые</span>
           <span className="text-xs text-gray-500">· {conversations.length}</span>
         </button>
         {!collapsed && (
           <div className="ml-5 mt-0.5 space-y-0.5">
             {conversations.map((conv) => (
               <Link
                 key={conv.id}
                 href={`/chat/${conv.id}`}
                 onClick={onNavigate}
                 className={cn(
                   'flex items-center gap-1 truncate rounded-md px-2 py-1 text-xs',
                   conv.id === activeConversationId
                     ? 'bg-gray-700 text-white'
                     : 'text-gray-400 hover:bg-gray-800 hover:text-white'
                 )}
               >
                 <span className="flex-1 truncate">{conv.title || 'Новый диалог'}</span>
                 {conv.projectId && projectsById[conv.projectId] && (
                   <ProjectChip
                     projectId={conv.projectId}
                     projectName={projectsById[conv.projectId].name}
                     size="xs"
                   />
                 )}
               </Link>
             ))}
           </div>
         )}
       </div>
     );
   }
   ```

4. Edit `services/frontend/components/sidebar/ProjectPane.tsx` (created in 19-01). Replace the `<div data-testid="pinned-section-slot" />` placeholder with the actual `<PinnedSection>` mounted with derived state:
   ```tsx
   const pinned = useMemo(
     () =>
       conversations
         .filter((c) => c.pinnedAt != null)
         .sort((a, b) => (b.pinnedAt ?? '').localeCompare(a.pinnedAt ?? '')),
     [conversations]
   );
   const projectsById = useMemo(
     () => Object.fromEntries(projects.map((p) => [p.id, { id: p.id, name: p.name }])),
     [projects]
   );
   // ...
   <PinnedSection
     conversations={pinned}
     projectsById={projectsById}
     activeConversationId={activeConversationId}
     onNavigate={onNavigate}
   />
   ```

5. Edit `services/frontend/components/sidebar/ProjectSection.tsx`:
   - Inside the chat-row `Link` map (around lines 86–100), wrap the `<Link>` in a `<DropdownMenu>` (or the file may already use a `<ContextMenu>` pattern — match what's there). In the menu items, add at the TOP:
     ```tsx
     {conv.pinnedAt == null ? (
       <DropdownMenuItem onClick={() => pinMutation.mutate(conv.id)}>Закрепить</DropdownMenuItem>
     ) : (
       <DropdownMenuItem onClick={() => unpinMutation.mutate(conv.id)}>Открепить</DropdownMenuItem>
     )}
     ```
   - In the `<Link>` body, add a small bookmark icon when pinned:
     ```tsx
     <span className="flex items-center gap-1">
       {conv.pinnedAt && <Bookmark size={10} className="text-yellow-400 shrink-0" />}
       <span className="truncate">{conv.title || 'Новый диалог'}</span>
     </span>
     ```
   - Use the new `usePinConversation()` and `useUnpinConversation()` hooks from `@/hooks/useConversations`.

6. Edit `services/frontend/components/sidebar/UnassignedBucket.tsx`. Apply the SAME context-menu and indicator extensions as ProjectSection.

7. Edit `services/frontend/components/chat/ChatHeader.tsx`:
   - Add a parallel narrow-selector hook (PATTERNS §23):
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
   - In the existing `ChatHeaderImpl` body (already memoized — line 58), call `const pinned = useConversationPinned(conversationId);` and render a bookmark button inside `rightSlot` (or create one if not present):
     ```tsx
     <button
       onClick={() => (pinned ? unpinMutation.mutate(conversationId) : pinMutation.mutate(conversationId))}
       aria-label={pinned ? 'Открепить чат' : 'Закрепить чат'}
       className="rounded-md p-1 hover:bg-gray-100"
     >
       <Bookmark
         size={16}
         className={pinned ? 'fill-yellow-400 text-yellow-400' : 'text-gray-400'}
       />
     </button>
     ```
   - Use `usePinConversation()` / `useUnpinConversation()` from `@/hooks/useConversations`.

8. Add tests:
   - `services/frontend/components/sidebar/__tests__/PinnedSection.test.tsx` — behaviors 4–7. Mirror `ProjectSection.test.tsx` wrapper pattern.
   - Extend `services/frontend/hooks/__tests__/useConversations.test.ts` (or create) — behaviors 1–3.
   - `services/frontend/components/chat/__tests__/ChatHeader.isolation.test.tsx` — behaviors 11–12 (extend if already exists; the file is referenced in PATTERNS §28 row).
   - Extend `services/frontend/components/sidebar/__tests__/ProjectSection.test.tsx` and `UnassignedBucket.test.tsx` (if exists; create otherwise) for behaviors 8–10.
   - `services/frontend/components/chat/__tests__/ProjectChip.test.tsx` — behavior 13 (verify default + xs class application).

9. Run:
   ```bash
   cd services/frontend && pnpm vitest run components/sidebar/__tests__/PinnedSection.test.tsx components/chat/__tests__/ChatHeader.isolation.test.tsx components/chat/__tests__/ProjectChip.test.tsx hooks/__tests__/useConversations.test.ts components/sidebar/__tests__/ProjectSection.test.tsx components/sidebar/__tests__/UnassignedBucket.test.tsx
   cd services/frontend && pnpm typecheck
   cd services/frontend && pnpm lint
   ```

   All must exit 0.
  </action>
  <verify>
    <automated>cd services/frontend && pnpm vitest run components/sidebar/__tests__/PinnedSection.test.tsx components/chat/__tests__/ChatHeader.isolation.test.tsx components/chat/__tests__/ProjectChip.test.tsx hooks/__tests__/useConversations.test.ts && pnpm typecheck</automated>
  </verify>
  <acceptance_criteria>
    - `services/frontend/components/sidebar/PinnedSection.tsx` exists, contains `'use client'`, contains `if (conversations.length === 0) return null;` (D-04)
    - `services/frontend/components/sidebar/PinnedSection.tsx` contains `«Закреплённые»` (Russian header)
    - `services/frontend/components/sidebar/PinnedSection.tsx` contains `size="xs"` (D-05 mini chip)
    - `services/frontend/hooks/useConversations.ts` contains `usePinConversation` AND `useUnpinConversation`
    - `services/frontend/hooks/useConversations.ts` contains `invalidateQueries` with `['conversations']`
    - `services/frontend/components/chat/ChatHeader.tsx` contains `useConversationPinned`
    - `services/frontend/components/chat/ChatHeader.tsx` contains `aria-label={pinned ? 'Открепить чат' : 'Закрепить чат'}`
    - `services/frontend/components/chat/ProjectChip.tsx` contains `sizeClasses` AND `size?: 'xs'`
    - `services/frontend/components/sidebar/ProjectSection.tsx` AND `UnassignedBucket.tsx` both contain `Закрепить` AND `Открепить`
    - `cd services/frontend && pnpm typecheck` exits 0
    - `cd services/frontend && pnpm vitest run components/sidebar/__tests__/PinnedSection.test.tsx` exits 0
    - `cd services/frontend && pnpm vitest run components/chat/__tests__/ChatHeader.isolation.test.tsx` exits 0
  </acceptance_criteria>
  <done>Pin mutations + PinnedSection + sidebar context-menu items + ChatHeader bookmark + ProjectChip size prop wired; flicker mitigation in place via narrow-memo selector; tests GREEN.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| client → API | `POST /conversations/{id}/pin` and `/unpin` carry bearer JWT; user-id and business-id derived server-side, never trusted from client. |
| API → MongoDB | Pin/Unpin filter `{_id, business_id, user_id}` — defense-in-depth scope filter. |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-19-02-01 | Tampering / Elevation of Privilege | `POST /conversations/{id}/pin` (cross-tenant pin) | mitigate | `Pin(ctx, id, businessID, userID)` repo signature requires both; `userID` and `businessID` resolved server-side from bearer JWT (NEVER from request body); MatchedCount==0 → ErrConversationNotFound → 404 (not 403, to avoid leaking existence vs ownership). |
| T-19-02-02 | Information Disclosure | Pin response leaks foreign conversation | accept | Handler returns 404 on cross-tenant attempts; uniform 404 vs ownership-aware 403 deliberately chosen (industry-standard guard against existence enumeration). |
| T-19-02-03 | Denial-of-service | Pin mutation flood | accept | v1.3 single-owner scale; rate limiting at gateway middleware (Phase 1 — already shipped). No new DoS surface. |

T-19-CROSS-TENANT, T-19-INDEX-503, T-19-LOG-LEAK belong to plan 19-03 (search) — not introduced here.
</threat_model>

<verification>
- `cd services/api && GOWORK=off go build ./...` succeeds
- `grep -q "BackfillConversationsV19" services/api/cmd/main.go` exits 0
- `cd services/api && GOWORK=off go test -race ./internal/repository/... ./internal/handler/... -run "TestPin|TestUnpin|TestBackfillConversationsV19|TestConversation_Pin|TestConversation_Unpin|TestEnsureConversationIndexes"` exits 0
- `cd services/frontend && pnpm typecheck && pnpm lint` succeeds
- `cd services/frontend && pnpm vitest run components/sidebar/__tests__/PinnedSection.test.tsx components/chat/__tests__/ChatHeader.isolation.test.tsx components/chat/__tests__/ProjectChip.test.tsx hooks/__tests__/useConversations.test.ts` exits 0
- Visual: starting the API on a fresh DB writes the schema_migrations marker `phase-19-search-sidebar-pinned-at`; restart is a no-op
- Visual: pinning a chat from sidebar context menu OR ChatHeader updates both surfaces; pinned chat appears in PinnedSection AND under its own project (D-05); ChatHeader bookmark turns yellow without flickering when other chats receive title updates (D-11 narrow-memo proven via isolation test)
</verification>

<success_criteria>
- `Conversation.PinnedAt` is the single source of truth (`Pinned bool` removed everywhere; backfill drops legacy field via $unset)
- BackfillConversationsV19 wired into `services/api/cmd/main.go` startup
- Pin/Unpin atomic methods scoped by `(business_id, user_id)` returning ErrConversationNotFound on mismatch
- `POST /conversations/{id}/pin` and `/unpin` registered in router
- New compound index `conversations_user_biz_proj_pinned_recency` created idempotently
- Frontend: `usePinConversation` + `useUnpinConversation` mutations invalidate `['conversations']`
- PinnedSection renders pinned chats with mini ProjectChip; hidden when empty
- ProjectSection + UnassignedBucket context menus include «Закрепить»/«Открепить» + bookmark indicator on pinned rows
- ChatHeader bookmark button uses `useConversationPinned` narrow-memo selector (D-11 mitigation)
- All Russian copy locked verbatim
- All cross-tenant scope filters in place
- All tests GREEN
- D-01, D-02, D-03, D-04, D-05 fully satisfied; UI-02 + UI-03 covered
</success_criteria>

<output>
After completion, create `.planning/phases/19-search-sidebar-redesign/19-02-SUMMARY.md` recording:
- The exact business-service field name on `ConversationHandler` used in Pin/Unpin handlers (`businessService` vs `businessSvc`)
- The exact path of the `ConversationRepository` interface where `Pin`/`Unpin` were added (`pkg/domain/...`)
- Whether `services/frontend/components/sidebar/__tests__/UnassignedBucket.test.tsx` already existed or was created
- The exact `data-message-id` semantic for Conversations across SSE flows (none — `data-message-id` is for messages, not conversations; just a check that nothing leaked)
</output>
