---
phase: 18
plan: 03
subsystem: api/repository + api/wiring
tags: [repository, mongo, atomic-update, indexes, title-status]
requirements: [TITLE-03, TITLE-04, TITLE-09]

dependency-graph:
  requires:
    - "Phase 15: Conversation.TitleStatus field + TitleStatus* enum constants"
    - "Phase 18 Plan 02: services/api/cmd/main.go index-block placeholder comment"
  provides:
    - "ConversationRepository.UpdateTitleIfPending(ctx, id, title) — TITLE-04 atomic conditional write"
    - "ConversationRepository.TransitionToAutoPending(ctx, id) — TITLE-09 atomic conditional flip"
    - "Update($set) extended to include title_status (D-06 plumbing fix / Landmine 7)"
    - "EnsureConversationIndexes — idempotent compound index helper, wired at API startup"
  affects:
    - "Plan 18-04 (titler service) — calls UpdateTitleIfPending on success path"
    - "Plan 18-05 (regenerate-title handler) — calls TransitionToAutoPending; also relies on D-06 Update plumbing for the PUT /conversations/{id} manual flip"

tech-stack:
  added: []
  patterns:
    - "Atomic conditional update via Mongo UpdateOne $in filter — manual rename mid-flight matches zero docs and the titler write is a no-op surfaced as ErrConversationNotFound"
    - "Idempotent compound-index helper at API startup (mirrors EnsurePendingToolCallsIndexes)"
    - "Fixture insertion via raw bson.M (not repo.Create) so the empty-status sentinel can produce a doc with title_status field absent — exercises legacy / pre-Phase-18 row eligibility"

key-files:
  created: []
  modified:
    - "pkg/domain/repository.go (+10 lines: ConversationRepository iface gains UpdateTitleIfPending + TransitionToAutoPending)"
    - "services/api/internal/repository/conversation.go (+95 lines: two new methods + EnsureConversationIndexes; Update $set extended for title_status)"
    - "services/api/internal/repository/conversation_test.go (+185 lines: 4 new tests + insertConvWithStatus helper)"
    - "services/api/cmd/main.go (+14 lines, -1 line: replace placeholder comment with EnsureConversationIndexes call)"
    - "services/api/internal/handler/conversation_test.go (+18 lines: MockConversationRepository extended with UpdateTitleIfPendingFunc + TransitionToAutoPendingFunc)"
    - "services/api/internal/handler/constructor_test.go (+6 lines: stubConversationRepo extended)"
    - "services/api/internal/handler/hitl_test.go (+2 lines: hitlConvRepo extended)"

key-decisions:
  - "$in:[..., nil] (NOT just $exists:false or status missing): the filter uses BSON null as a list element. With Conversation.TitleStatus tagged WITHOUT bson `omitempty`, legacy docs serialized with `title_status: null` AND any future explicit-null write are both reachable; this is the only stable cross-driver shape that handles both the missing-field and explicit-null cases."
  - "TransitionToAutoPending touches title_status ONLY (not title): the status flip is metadata; preserves whatever title was there for the brief window before the new titler run finishes."
  - "Update method extended in place rather than introducing a new UpdateRename method: D-06 says PUT renames are sovereign, the rename path IS the Update method, so it must persist the manual flag. Adding a second method would split the rename-persistence surface unnecessarily."
  - "Test fixture insertion bypasses repo.Create: lets us produce the absent-title_status legacy-row case authentically (Create always writes the zero-value empty string, which is NOT the same as field absent for the $in filter)."

metrics:
  duration: "~6min"
  completed: "2026-04-26T18:24:28Z"
  tasks: 2
  commits: 2
  files_created: 0
  files_modified: 7
---

# Phase 18 Plan 03: Atomic Title Persistence Primitives Summary

The trust-critical persistence layer for Phase 18 is now in place: two new
atomic ConversationRepository methods, the D-06 plumbing fix in Update, and
the D-08a compound index helper wired at API startup — closing the bug class
that PITFALLS §12 flags as the trust-critical failure mode.

## Final ConversationRepository Surface (Phase 18 additions)

Both new methods live in `services/api/internal/repository/conversation.go`
and are declared on the interface in `pkg/domain/repository.go:64-73`.

| Method                      | File:Line                                          | Filter (BSON)                                                       | $set (BSON)                                                                  |
| --------------------------- | -------------------------------------------------- | ------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| `UpdateTitleIfPending`      | `services/api/internal/repository/conversation.go:155` | `{_id, title_status: {$in: [TitleStatusAutoPending, nil]}}`         | `{title, title_status: TitleStatusAuto, updated_at}`                         |
| `TransitionToAutoPending`   | `services/api/internal/repository/conversation.go:186` | `{_id, title_status: {$in: [TitleStatusAuto, nil]}}`                | `{title_status: TitleStatusAutoPending, updated_at}`                         |

Both methods return `domain.ErrConversationNotFound` when `MatchedCount == 0`.
Callers in Plan 04 (titler) and Plan 05 (regenerate handler) pre-read via
`GetByID` to disambiguate "doc deleted" from "race lost" / "manual sovereign".

## D-06 Plumbing Fix (Landmine 7)

`services/api/internal/repository/conversation.go:78-99` — the existing
`Update` method's `$set` block is extended:

```go
update := bson.M{
    "$set": bson.M{
        "user_id":      conv.UserID,
        "title":        conv.Title,
        "title_status": conv.TitleStatus, // D-06 plumbing: rename path persists status flip
        "updated_at":   conv.UpdatedAt,
    },
}
```

Without this fix, Plan 05's handler-level `conversation.TitleStatus = "manual"`
mutation would be silently dropped at the repo layer and an in-flight titler
could clobber the user's chosen title. `TestUpdate_PersistsTitleStatus`
guards against the bug returning.

## D-08a Index Wiring

`services/api/cmd/main.go:118-129` — replaces the Plan 02 placeholder comment
with the actual `EnsureConversationIndexes(indexesCtx2, mongoDB)` call. The
block mirrors the existing `EnsurePendingToolCallsIndexes` lifecycle 1:1
(separate 30s timeout context, early-return on error, defer-style cancel).

The created index spec:

```go
mongo.IndexModel{
    Keys: bson.D{
        {Key: "user_id",      Value: 1},
        {Key: "business_id",  Value: 1},
        {Key: "title_status", Value: 1},
    },
    Options: options.Index().SetName("conversations_user_biz_title_status"),
}
```

`EnsureConversationIndexes` lives at `services/api/internal/repository/conversation.go:224-241`.

## Landmine 8 Guard (omitempty on TitleStatus must NOT exist)

```bash
$ grep -E "TitleStatus.*omitempty" pkg/domain/mongo_models.go
# (no output — 0 matches)
```

The `$in: [..., nil]` filter on `UpdateTitleIfPending` and
`TransitionToAutoPending` depends on `title_status` being reachable as `null`
on legacy docs. If `omitempty` were ever added to the BSON tag, legacy docs
would have the field absent (not null) and the $in would still match in the
v2 driver (Mongo treats absent-and-null identically for `$in`), but it would
also lose the explicit-null write semantics that move-chat (Phase 15)
relies on for the project_id pattern. The tag intentionally omits
`omitempty`; this plan does not touch `pkg/domain/mongo_models.go`.

## Test Coverage Summary

| Test                                       | File:Line                                              | Branches Covered                                                                                                                                                                                                  |
| ------------------------------------------ | ------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `TestUpdateTitleIfPending`                 | `conversation_test.go:437`                             | success(status=auto_pending) · success(status=null/legacy) · no-op(status=manual race lost) · no-op(status=auto already terminal) · missing id                                                                    |
| `TestTransitionToAutoPending`              | `conversation_test.go:487`                             | success(status=auto) · success(status=null/legacy) · no-op(status=manual sovereign per D-02) · no-op(status=auto_pending in-flight per D-03) · missing id · title field MUST be untouched on every branch          |
| `TestUpdate_PersistsTitleStatus`           | `conversation_test.go:535`                             | Landmine 7 regression: simulates Plan 05 PUT handler flow (read → flip TitleStatus to "manual" → Update → re-fetch); fails if `$set` block ever loses `title_status`                                              |
| `TestEnsureConversationIndexes_Idempotent` | `conversation_test.go:559`                             | first-call success · second-call no-op · named index `conversations_user_biz_title_status` exists in `Indexes().ListSpecifications`                                                                                |

15 sub-test cases total. All pass against live Mongo:
`go test -race -count=1 ./internal/repository/...` → ok 1.340s.
Full services/api suite green: `go test -race -count=1 ./...` → all 7
test packages ok.

## Verification Results

- `cd services/api && GOWORK=off go build ./...` — exit 0
- `cd pkg && GOWORK=off go build ./...` — exit 0
- `cd services/api && GOWORK=off go vet ./...` — exit 0
- `cd pkg && GOWORK=off go vet ./...` — exit 0
- `cd services/api && GOWORK=off go test -race -count=1 ./internal/repository/...` — ok 1.859s
- `cd services/api && GOWORK=off go test -race -count=1 ./...` — ok across all 7 test packages
- `grep -E "TitleStatus.*omitempty" pkg/domain/mongo_models.go` — 0 matches (Landmine 8 guard)
- All 11 acceptance-criteria greps from PLAN.md return the expected counts (with one
  documented deviation, see below)

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Test mocks did NOT implement the extended ConversationRepository interface**

- **Found during:** Task 1 verification (`go vet ./...` after the iface extension)
- **Issue:** `services/api/internal/handler/conversation_test.go::MockConversationRepository`,
  `services/api/internal/handler/constructor_test.go::stubConversationRepo`, and
  `services/api/internal/handler/hitl_test.go::hitlConvRepo` are existing test
  doubles for `domain.ConversationRepository`. Adding `UpdateTitleIfPending` and
  `TransitionToAutoPending` to the interface broke 9+ test files in
  `services/api/internal/handler/...` because the mocks no longer satisfied the
  contract. This is the canonical blocking-issue shape (interface extension
  cascades through test infrastructure).
- **Fix:** Extended each of the three mock types with both new methods. Added
  `UpdateTitleIfPendingFunc` and `TransitionToAutoPendingFunc` `*Func` overrides
  on `MockConversationRepository` (the only one that already used the
  function-field override pattern); the other two are stubs that return `nil`
  unconditionally because no handler test currently exercises Plan 04/05 paths.
- **Files modified:** `services/api/internal/handler/conversation_test.go`,
  `services/api/internal/handler/constructor_test.go`,
  `services/api/internal/handler/hitl_test.go`
- **Commit:** `83165cc` (folded into Task 1 commit since it is a
  necessary-and-sufficient consequence of the iface extension)
- **Verification:** `go vet ./...` clean across services/api after the addition.

**2. [Rule 1 - Bug] Plan acceptance grep `domain.TitleStatusAutoPending, nil` count discrepancy**

- **Found during:** Task 1 acceptance-criteria sweep
- **Issue:** Plan 18-03 acceptance criterion expected
  `grep -nF "domain.TitleStatusAutoPending, nil" services/api/internal/repository/conversation.go`
  to return 2 matches ("one in each of the two atomic methods"). The plan's
  own implementation snippet (and the correct semantics enshrined in landmine
  notes from the prompt) uses
  `[TitleStatusAutoPending, nil]` for `UpdateTitleIfPending` and
  `[TitleStatusAuto, nil]` for `TransitionToAutoPending` — i.e. each method
  has a different first element of the $in array. The literal byte sequence
  `domain.TitleStatusAutoPending, nil` therefore appears exactly once
  (in `UpdateTitleIfPending`); the literal `domain.TitleStatusAuto, nil`
  appears exactly once (in `TransitionToAutoPending`). The plan acceptance
  grep was a textual misread of the plan's own action steps.
- **Fix:** Functional contract takes precedence over the textual grep. Both
  methods are implemented byte-identical to the plan's `<action>` Step 2
  source code; the actual landmine semantics from the prompt
  ("UpdateTitleIfPending filter: `$in: [auto_pending, null]`",
  "TransitionToAutoPending filter: `$in: [auto, null]`") are honored. The
  alternative grep
  `grep -cF "domain.TitleStatusAuto, nil" services/api/internal/repository/conversation.go`
  returns 1 match, confirming the TransitionToAutoPending filter is correct.
- **Files modified:** none (plan acceptance criterion mismatch with plan body)
- **Verification:** Functional integration tests cover both branches end-to-end:
  `TestUpdateTitleIfPending` exercises the auto_pending and null paths
  (both succeed with status flipped to "auto"); `TestTransitionToAutoPending`
  exercises the auto and null paths (both succeed with status flipped to
  "auto_pending"). Manual / already-terminal paths fail closed with
  `ErrConversationNotFound`. The trust contract (manual sovereign, atomic race-loss
  fail-soft) is proven by integration tests, not by literal grep counts.

### Out-of-scope discoveries

None — both tasks executed exactly as written in the PLAN action steps, with
the two auto-fixes above as necessary mitigations for issues induced by the
plan's own structural changes.

## Issues Encountered

None outside the two deviations above. Mongo was available locally for
integration testing (`mongodb://localhost:27017`); all 15 sub-tests pass
green on first run.

## Self-Check: PASSED

Created files exist:
- N/A — this plan modifies existing files only

Modified files exist with expected content:
- FOUND: `pkg/domain/repository.go` — UpdateTitleIfPending at line 68, TransitionToAutoPending at line 73
- FOUND: `services/api/internal/repository/conversation.go` — UpdateTitleIfPending at line 155, TransitionToAutoPending at line 186, EnsureConversationIndexes at line 224, D-06 plumbing fix at line 91
- FOUND: `services/api/internal/repository/conversation_test.go` — 4 new test functions at lines 437, 487, 535, 559
- FOUND: `services/api/cmd/main.go` — EnsureConversationIndexes call at line 125, placeholder comment removed (0 matches)
- FOUND: `services/api/internal/handler/conversation_test.go` — MockConversationRepository extended with new *Func fields + methods
- FOUND: `services/api/internal/handler/constructor_test.go` — stubConversationRepo extended
- FOUND: `services/api/internal/handler/hitl_test.go` — hitlConvRepo extended

Commits exist:
- FOUND: `83165cc` — feat(18-03): atomic conversation title primitives + index helper (Task 1)
- FOUND: `16ca766` — test(18-03): integration tests for atomic title primitives + main.go index wiring (Task 2)

Acceptance verifications:
- `cd services/api && GOWORK=off go build ./...` exits 0 ✓
- `cd services/api && GOWORK=off go vet ./...` exits 0 ✓
- `cd services/api && GOWORK=off go test -race -count=1 ./internal/repository/...` exits 0 ✓
- `cd services/api && GOWORK=off go test -race -count=1 ./...` exits 0 ✓
- `grep -E "TitleStatus.*omitempty" pkg/domain/mongo_models.go` returns 0 matches ✓ (Landmine 8 guard)

## Threat Flags

None. Plan 18-03's surface (repository extension + index helper + main.go
wiring) is exactly the surface enumerated in the threat_model:

- **T-18-02 (Tampering: manual rename clobber):** mitigated. Mongo `$in`
  filter on `title_status` ensures the titler write only matches docs where
  status is "auto_pending" or null. The integration test
  `TestUpdateTitleIfPending` case "no-op: status=manual (race lost)" enforces
  the contract: assertion `title MUST be untouched on filter-fail (manual
  won race)` is the exact phrasing of the threat mitigation.
- **T-18-03 (Tampering: regenerate clobber of manual):** mitigated.
  `TransitionToAutoPending` filter excludes status="manual"; manual rows
  reject the transition with `ErrConversationNotFound`. Test
  "no-op: status=manual (sovereign per D-02)" enforces.
- **T-18-07 (Tampering: D-06 plumbing failure → handler-level "manual" flip
  silently dropped):** mitigated. `Update` method's `$set` block now
  includes `title_status: conv.TitleStatus`. `TestUpdate_PersistsTitleStatus`
  regression-tests against the original bug. Without this fix T-18-02 would
  be defeated downstream.

No new network endpoints, auth paths, file-access patterns, or schema
changes at trust boundaries introduced. The compound index is a performance
primitive; it does not alter the security model.
