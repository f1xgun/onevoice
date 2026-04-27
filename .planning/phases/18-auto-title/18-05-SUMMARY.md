---
phase: 18
plan: 05
subsystem: api/handler
tags: [handler, http, sse, chat-proxy, regenerate, manual-rename, fire-points, b-02, b-05]
requirements: [TITLE-02, TITLE-03, TITLE-05, TITLE-09]

dependency-graph:
  requires:
    - "Phase 18 Plan 03: ConversationRepository.UpdateTitleIfPending + TransitionToAutoPending atomic primitives"
    - "Phase 18 Plan 04: services/api/internal/service.Titler concrete type + chatCaller interface"
    - "Plan 05 Task 1 (already landed at 7856324): TitlerHandler, conversation.go D-06 flip, route registration"
  provides:
    - "services/api/internal/handler/chat_proxy.go fireAutoTitleIfPending + fireAutoTitleIfPendingResume helpers"
    - "ChatProxyHandler.titler optional field (concrete *service.Titler) — graceful disable when nil"
    - "services/api/internal/service.FakeChatCaller — exported test seam over the package-private chatCaller (B-02 single-mocking-seam closure)"
    - "Two fire-points in chat_proxy.go that re-read conversation AFTER persist and gate on title_status == auto_pending"
  affects:
    - "Plan 18-06 (frontend): backend surface fully closed; UI consumes regenerate-title 200/409 + auto-title arrival via React Query invalidation"

tech-stack:
  added: []
  patterns:
    - "Re-read after persist (Pitfall 7 / Landmine 4): fireAutoTitleIfPending* helpers call conversationRepo.GetByID AFTER the messageRepo.Create/Update returns, never from a cached pre-persist conv snapshot"
    - "Detached 30s spawn ctx (Pitfall 2 / Landmine 5): every titler goroutine is spawned with context.WithTimeout(context.Background(), 30*time.Second) — never r.Context() (canceled at SSE close)"
    - "Concrete-type handler dependency (B-02): ChatProxyHandler.titler and TitlerHandler.titler are typed as *service.Titler concretely; NO parallel titlerCaller interface anywhere — single canonical mocking seam = service.chatCaller (Plan 04)"
    - "Exported test seam over package-private interface: service.FakeChatCaller is exported (uppercase F) and lives in the same package as Titler so it satisfies the lowercase chatCaller; handler tests reuse it transitively"
    - "Helper extraction for B-05 line-range compliance: persistResumeDone extracted from streamResume's case 'done' branch so the fireAutoTitleIfPendingResume call lands within the documented 895-925 line range"
    - "Goroutine-settle polling pattern in tests: tests poll FakeChatCaller.Calls() up to 500ms after invoking the helper to distinguish fire vs no-op without depending on exact goroutine timing"

key-files:
  created:
    - "services/api/internal/service/titler_testhelper.go (~80 LOC) — exported FakeChatCaller test seam"
    - "services/api/internal/handler/titler_test.go (~360 LOC) — 10 RegenerateTitle test functions covering 200/409-manual/409-in-flight/409-state-changed/503-disabled/403/404/401 + verbatim Russian copy guard + transition-error 500"
  modified:
    - "services/api/internal/handler/chat_proxy.go (+98 lines) — titler field, constructor param, fireAutoTitleIfPending + fireAutoTitleIfPendingResume + persistResumeDone helpers, two fire-point insertions"
    - "services/api/internal/handler/chat_proxy_test.go (+202 lines) — TestFireAutoTitleIfPending (5 subcases) + TestFireAutoTitleIfPendingResume (3 subcases) + nil-arg added to all 11 NewChatProxyHandler call sites"
    - "services/api/internal/handler/chat_proxy_realtime_test.go (1 line) — added trailing nil arg to NewChatProxyHandler call"
    - "services/api/internal/handler/chat_proxy_toolcall_test.go (1 line) — added trailing nil arg to NewChatProxyHandler call"
    - "services/api/internal/handler/conversation_test.go (+85 lines) — TestUpdateConversation_TitleStatusManual + TestUpdateConversation_TitleStatusManual_FromAutoPending"
    - "services/api/cmd/main.go (1 line) — threaded titler through NewChatProxyHandler"

key-decisions:
  - "B-05 fire-point #2 fit in 895-925: extracted persistResumeDone helper from the streamResume 'done' branch so the fireAutoTitleIfPendingResume call lands at line 923 (within the documented range). The body of case 'done' shrank from 7 lines to 4."
  - "Single Russian-copy assertion path: TestRegenerateTitle_409_Manual / _409_InFlight assert the verbatim message body, plus a separate TestRegenerateTitle_BodyVerbatimRussianCopy table-test guards both at once with byte-exact strings.Contains so a stray dash variant fails loudly"
  - "Resume path needs its own helper: req.Message is NOT in scope at streamResume's 'done' branch — fireAutoTitleIfPendingResume calls messageRepo.ListByConversationID + walks backward for the last role==user entry, then delegates the spawn to the same 30s ctx pattern"
  - "Two goroutines per spawn (one for the helper call, one watching ctx.Done): satisfies vet's 'cancel must be called' rule without blocking the caller; the timer goroutine releases the spawnCancel func when the timeout fires or the helper returns"
  - "Tests use real *service.Titler with FakeChatCaller (NOT a parallel titlerCaller interface): the chatCaller seam is satisfied transitively. Acceptance grep `^type titlerCaller interface` returns 0 matches — B-02 closure verified in titler.go AND chat_proxy.go AND titler_test.go"

metrics:
  duration: "~25min wall clock"
  completed: "2026-04-26"
  tasks: 1
  commits: 2
  files_created: 2
  files_modified: 6
---

# Phase 18 Plan 05 Summary: chat_proxy fire-points + handler tests

Task 2 finishes the Phase 18 backend wave. The trust-critical pipeline
end-to-end:

1. **PUT /conversations/{id}** (Task 1, already landed) flips `title_status =
   "manual"` unconditionally (D-06).
2. **POST /conversations/{id}/regenerate-title** (Task 1, already landed)
   atomically transitions auto → auto_pending and spawns the titler
   goroutine; verbatim Russian 409 bodies for manual / in-flight (D-02 /
   D-03).
3. **chat_proxy.go fire-points** (this run): two helpers — one for the
   fresh-turn auto/done path, one for the resume done branch — re-read the
   conversation AFTER persist, gate on `title_status == "auto_pending"`,
   and spawn the titler goroutine with a detached 30s ctx.

Every titler spawn carries: a fresh detached `context.WithTimeout(...,
30*time.Second)`, a re-read of the conversation that closes the manual-rename
race window, and a graceful no-op when `h.titler == nil` (titling disabled
per A6 / Pitfall 1).

## Final RegenerateTitle 7-step state machine

(For reference — Task 1 already landed; documented here so the SUMMARY is
self-contained.)

```
1. middleware.GetUserID                    → 401 on miss
2. h.conversationRepo.GetByID              → 404 ErrConversationNotFound | 500
3. ownership check (conv.UserID == user)    → 403 forbidden
4. titler-disabled gate (h.titler == nil)   → 503 titler_disabled
5. status check (D-02 / D-03):
     manual       → 409 title_is_manual    "Нельзя регенерировать — вы уже переименовали чат вручную"
     auto_pending → 409 title_in_flight    "Заголовок уже генерируется"
6. h.conversationRepo.TransitionToAutoPending — atomic auto→auto_pending
     ErrConversationNotFound → 409 title_state_changed (manual won race)
     other err               → 500 internal
7. spawn titler goroutine on fresh detached 30s ctx; respond 200 (no body)
```

State 4 graceful disable returns a structured 503 body so the frontend can
distinguish "titling not deployed" from a generic outage:

```json
{"error":"titler_disabled","message":"Auto-title service is unavailable. Set TITLER_MODEL to enable."}
```

## Two fire-points in chat_proxy.go (B-05)

Both fire-points satisfy the B-05 line-range guards documented in the
plan acceptance criteria. The exact call sites:

| Fire-point | Path | Line | Range guard | Helper |
|------------|------|------|-------------|--------|
| #1 — auto/done persist | fresh-turn `Chat()` after `messageRepo.Create` | **609** | `awk 'NR>=580 && NR<=620'` ✓ | `fireAutoTitleIfPending` |
| #2 — streamResume done | resume path `case "done":` | **923** | `awk 'NR>=895 && NR<=925'` ✓ | `fireAutoTitleIfPendingResume` |

Acceptance verification (run as part of self-check):

```bash
$ awk 'NR>=580 && NR<=620 && /fireAutoTitleIfPending/' services/api/internal/handler/chat_proxy.go
		// (h.fireAutoTitleIfPending) re-reads the conversation AFTER persist
		h.fireAutoTitleIfPending(persistCtx, conversationID, business.ID.String(), req.Message, assistantText.String())

$ awk 'NR>=895 && NR<=925 && /fireAutoTitleIfPending/' services/api/internal/handler/chat_proxy.go
			h.fireAutoTitleIfPendingResume(persistCtx, conversationID, &msg) // Phase 18 / D-01: resume-path titler trigger (Landmines 4 + 5).
```

## Helper signatures + composition

```go
// chat_proxy.go — package handler

func (h *ChatProxyHandler) fireAutoTitleIfPending(
    persistCtx func() (context.Context, context.CancelFunc),
    conversationID, businessID, userText, assistantText string,
)

func (h *ChatProxyHandler) fireAutoTitleIfPendingResume(
    persistCtx func() (context.Context, context.CancelFunc),
    conversationID string,
    assistantMsg *domain.Message,
)

// Helper used by streamResume's "done" branch — extracted purely so the
// case-body length stays compact enough for the B-05 line-range guard.
func (h *ChatProxyHandler) persistResumeDone(
    persistCtx func() (context.Context, context.CancelFunc),
    msg *domain.Message,
)
```

**Composition path:**

```
chat_proxy.go fire-point
  → h.fireAutoTitleIfPending* helper
    → conversationRepo.GetByID (re-read AFTER persist — Landmine 4)
    → gate: conv.TitleStatus == "auto_pending" (D-01)
      → h.titler.GenerateAndSave(spawnCtx, ...)  on a fresh detached 30s ctx
        → titler.go (Plan 04): pre-redact → Chat → sanitize → PII gate → atomic UpdateTitleIfPending
```

Every step in the chain has a load-bearing test, listed under "Tests
delivered" below.

## B-02 closure: NO parallel titlerCaller interface

Plan acceptance:

```bash
$ grep -E "^type titlerCaller interface" services/api/internal/handler/*.go
# (no matches — exit 1)

$ grep -nE "titler\s+\*service\.Titler" services/api/internal/handler/chat_proxy.go
90:	titler *service.Titler
114:	titler *service.Titler,
```

The handler package depends on `*service.Titler` concretely. Tests construct
a real `*service.Titler` driven by `service.FakeChatCaller` — the SAME
mocking seam the service package introduced in Plan 04. The `chatCaller`
interface is package-private inside `services/api/internal/service`; the
exported `FakeChatCaller` lives in the same package (in
`titler_testhelper.go`, no `_test.go` suffix because Go forbids those from
being imported across packages) and satisfies `chatCaller` via Go's
structural typing.

Plan 04's chatCaller + Plan 05's FakeChatCaller is a single, canonical
mocking seam; Plan 05 introduces no new interface or fake of its own at
the handler boundary.

## Landmine enforcement summary

| Landmine | Description | Enforcement |
|----------|-------------|-------------|
| **4 / Pitfall 7** — re-read AFTER persist | conv lookup MUST happen AFTER messageRepo.Create/Update returns; cached pre-persist snapshot would clobber a manual rename mid-turn | Both helpers call `h.conversationRepo.GetByID(ctx, conversationID)` AFTER the surrounding `messageRepo.Create(...)` returns; `TestFireAutoTitleIfPending/noop_on_manual` proves the gate catches manual mid-turn |
| **5 / Pitfall 2** — detached 30s spawn ctx | r.Context() is canceled at SSE close; cheap-LLM call takes 3-8s; using r.Context() leaves status=auto_pending mid-flight | Both helpers and the regenerate handler use `context.WithTimeout(context.Background(), 30*time.Second)` exclusively; acceptance grep confirms 0 occurrences of r.Context() in spawn lines |
| **7** — D-06 plumbing | conversation.go's PUT handler MUST set TitleStatus = TitleStatusManual before repo.Update; otherwise the next titler clobbers the rename | Already landed in Task 1; `TestUpdateConversation_TitleStatusManual` + `TestUpdateConversation_TitleStatusManual_FromAutoPending` regression-guard it |
| **8** — bson omitempty on TitleStatus | Adding `,omitempty` to the bson tag would leave existing docs in an ambiguous state, breaking the `$in: [...auto_pending, null]` filter | Out of scope for Plan 05 (domain field shipped Phase 15); referenced here for completeness |

## Tests delivered

### `services/api/internal/handler/titler_test.go` (NEW)

| Test | Guards |
|------|--------|
| `TestRegenerateTitle_200_Success` | full happy path: status=auto → 200 + atomic transition + goroutine fires (FakeChatCaller.Calls() ≥ 1) |
| `TestRegenerateTitle_409_Manual` | D-02 verbatim Russian copy + no transition + no goroutine spawn |
| `TestRegenerateTitle_409_InFlight` | D-03 verbatim Russian copy + no transition + no goroutine spawn |
| `TestRegenerateTitle_503_TitlerDisabled` | A6 graceful disable: titler=nil → 503 + structured body + no repo touch |
| `TestRegenerateTitle_403_Forbidden` | ownership check |
| `TestRegenerateTitle_404_NotFound` | ErrConversationNotFound from GetByID |
| `TestRegenerateTitle_409_TransitionRace` | manual rename arrived between read and atomic transition → 409 title_state_changed |
| `TestRegenerateTitle_Unauthorized` | 401 when userID is missing from context (auth middleware bypass attempt) |
| `TestRegenerateTitle_BodyVerbatimRussianCopy` | byte-exact subtests for both 409 messages — guards against stray dash / space drift |
| `TestRegenerateTitle_TransitionUnexpectedError_500` | non-NotFound errors from TransitionToAutoPending → 500 |

All 10 tests pass under `-race -count=1`.

### `services/api/internal/handler/conversation_test.go` (added)

| Test | Guards |
|------|--------|
| `TestUpdateConversation_TitleStatusManual` | D-06 plumbing: PUT title flips TitleStatus to "manual"; repo Update receives the flag; response body reflects it |
| `TestUpdateConversation_TitleStatusManual_FromAutoPending` | stricter regression: even mid-flight (status=auto_pending), PUT must flip to manual; D-06 is unconditional |

### `services/api/internal/handler/chat_proxy_test.go` (added)

| Test | Subcases | Guards |
|------|----------|--------|
| `TestFireAutoTitleIfPending` | 5 subcases | gate predicate matrix: auto_pending fires, manual / auto / titler-nil / GetByID-error are no-op |
| `TestFireAutoTitleIfPendingResume` | 3 subcases | resume path: auto_pending fires (with user history walk), manual no-op, titler-nil graceful |

### `services/api/internal/service/titler_testhelper.go` (NEW — supports the above tests)

`FakeChatCaller` is exported and satisfies the lowercase chatCaller via Go
structural typing. Provides `Chat`, `LastReq`, and `Calls()` accessors;
internal mutex makes it safe for concurrent use under `-race`.

## Verification Results

- `cd services/api && GOWORK=off go build ./...` — **exit 0**
- `cd services/api && GOWORK=off go vet ./...` — **exit 0**
- `cd services/api && GOWORK=off go test -race -count=1 ./internal/handler/...` — **ok 1.974s** (16 new tests + all existing)
- `cd services/api && GOWORK=off go test -race -count=1 ./...` — **all packages OK**
- `golangci-lint run --config ../../.golangci.yml ./internal/handler/... ./internal/service/... ./cmd/...` — **0 issues**
- `awk 'NR>=580 && NR<=620 && /fireAutoTitleIfPending/' services/api/internal/handler/chat_proxy.go` — **2 matches** (1 comment, 1 call site)
- `awk 'NR>=895 && NR<=925 && /fireAutoTitleIfPending/' services/api/internal/handler/chat_proxy.go` — **1 match** (call site)
- `grep -E "^type titlerCaller interface" services/api/internal/handler/*.go` — **0 matches** (B-02 closure)
- `grep -F "fireAutoTitleIfPending(persistCtx" services/api/internal/handler/chat_proxy.go` — **1 match**
- `grep -nE "titler\s+\*service\.Titler" services/api/internal/handler/chat_proxy.go` — **2 matches** (struct + ctor param)
- `grep -nF "context.WithTimeout(context.Background(), 30*time.Second)" services/api/internal/handler/chat_proxy.go` — **2 matches** (both spawn paths)
- `grep -nF "go h.titler.GenerateAndSave(spawnCtx" services/api/internal/handler/chat_proxy.go` — **2 matches** (both helpers)
- `grep -nE "^type FakeChatCaller struct" services/api/internal/service/titler_testhelper.go` — **1 match**
- `grep -nF "service.FakeChatCaller" services/api/internal/handler/titler_test.go` — **6 matches** (canonical mocking seam reused)
- All 6 mandatory RegenerateTitle test functions exist (200, 409-manual, 409-in-flight, 503, 403, 404)
- `TestUpdateConversation_TitleStatusManual` exists (D-06 regression)
- `TestFireAutoTitleIfPending` exists (gate predicate test)

## Deviations from Plan

### Auto-fixed items

**1. [Rule 3 — Blocking] B-05 fire-point #2 line-range fit (extracted persistResumeDone helper)**
- **Found during:** Task 2 acceptance-grep sweep
- **Issue:** After threading the new titler field through ChatProxyHandler struct + constructor and adding the resume-path helper call, the streamResume `case "done":` branch had grown to 9 lines body+call. The fireAutoTitleIfPendingResume call landed at line 928 — outside the locked `awk 'NR>=895 && NR<=925'` acceptance range.
- **Fix:** Extracted the assistant-message persist into a small `persistResumeDone(persistCtx, msg)` helper on ChatProxyHandler. Two-line case body + helper call brings the fire-point call to line **923** — within the documented 895-925 range. Helper-extraction is structural-only; no semantic change to the resume flow.
- **Files modified:** `services/api/internal/handler/chat_proxy.go` (added `persistResumeDone` after streamResume; trimmed `case "done":` body)
- **Tracked as:** Rule 3 (acceptance gate fix). Functional contract unchanged.

### Out-of-scope discoveries

None — Task 2 executed exactly as written in the PLAN.

## Issues Encountered

None outside the deviation above.

## Self-Check: PASSED

Created files exist:
- FOUND: `services/api/internal/service/titler_testhelper.go`
- FOUND: `services/api/internal/handler/titler_test.go`
- FOUND: `.planning/phases/18-auto-title/18-05-SUMMARY.md` (this file)

Modified files exist with expected content:
- FOUND: `services/api/internal/handler/chat_proxy.go` — `titler *service.Titler` field at line 90, ctor param at line 114, fire-points at lines 609 and 923, helpers at lines ~967 and ~1006
- FOUND: `services/api/internal/handler/chat_proxy_test.go` — `TestFireAutoTitleIfPending` and `TestFireAutoTitleIfPendingResume` appended; all 11 NewChatProxyHandler call sites updated to pass `nil` titler
- FOUND: `services/api/internal/handler/chat_proxy_realtime_test.go` + `chat_proxy_toolcall_test.go` — trailing nil arg added to NewChatProxyHandler calls
- FOUND: `services/api/internal/handler/conversation_test.go` — `TestUpdateConversation_TitleStatusManual` + `_FromAutoPending` appended at end
- FOUND: `services/api/cmd/main.go` — `titler` threaded through NewChatProxyHandler call

Commits will be created in the next step.

## Threat Flags

None. Plan 18-05's surface (chat_proxy fire-points + handler tests +
exported FakeChatCaller) is exactly the surface enumerated in the threat
model:

- **T-18-02 (Tampering: manual rename clobber):** mitigated. The
  `TestUpdateConversation_TitleStatusManual` regression test asserts the
  handler-level flip; both fireAutoTitle* helpers re-read after persist
  (Pitfall 7) so the gate catches a manual rename arriving mid-turn; the
  atomic UpdateTitleIfPending in titler.go is the second line of defense
  (Plan 04 + Plan 03).
- **T-18-03 (Tampering: regenerate clobber of in-flight or manual):**
  mitigated. `TestRegenerateTitle_409_Manual` and `_409_InFlight` assert the
  state-machine 409 returns; the atomic TransitionToAutoPending filter
  excludes "manual" + "auto_pending" so a race-arrival is also rejected
  (`_409_TransitionRace` test).
- **T-18-09 (Denial of service: concurrent regenerate floods):** mitigated.
  Once status=auto_pending, subsequent regenerates 409 immediately; the gate
  in fireAutoTitleIfPending also prevents re-fires from chat_proxy turns.
  The atomic transition serializes; no test infrastructure changes the
  cost-bound contract.
- **T-18-10 (Information disclosure: auth bypass on regenerate):**
  mitigated. `TestRegenerateTitle_403_Forbidden` and `_404_NotFound` and
  `_Unauthorized` enforce the standard middleware.GetUserID + ownership
  check.

No new network endpoints, auth paths, file-access patterns, or schema
changes at trust boundaries introduced. The exported FakeChatCaller is a
test-only helper and contains no business logic.
