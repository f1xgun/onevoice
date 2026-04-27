---
phase: 18-auto-title
verified: 2026-04-26T20:00:00Z
status: passed
score: 9/9 must-haves verified
overrides_applied: 0
re_verification:
  previous_status: none
  previous_score: N/A
  gaps_closed: []
  gaps_remaining: []
  regressions: []
human_verification:
  - test: "End-to-end smoke: fresh chat -> assistant reply -> sidebar title updates without flicker"
    expected: "Sidebar shows 'Новый диалог' on chat creation. After first assistant reply completes (chat SSE 'done'), the sidebar title transitions to a 3-6 word Russian title within ~5-10s. Composer focus is preserved, message-list scroll is unaffected."
    why_human: "TITLE-06 mandates 'no flicker, no composer focus loss' — these are visual / UX qualities that the structural mitigation (D-11, B-06) reduces but cannot fully prove offline."
  - test: "Manual rename arrives mid-flight while titler is in progress"
    expected: "User opens new chat, sends first message, before assistant reply lands clicks 'Переименовать' to set a manual title. After SSE 'done' the manual title persists; the in-flight titler write is a silent no-op (atomic UpdateTitleIfPending matches zero docs)."
    why_human: "Race-window timing depends on a real cheap LLM round-trip (~3-8s); only manual exercise proves the trust-critical 'manual sovereign' contract end-to-end."
  - test: "Regenerate flow exactly once, silent failure"
    expected: "From the sidebar kebab menu, click 'Обновить заголовок' on a non-manual chat. The title transitions auto -> auto_pending -> auto with a new 3-6 word Russian title. Subsequent click during in-flight returns 409 toast 'Заголовок уже генерируется'. After manual rename, the menu item disappears entirely."
    why_human: "TITLE-09 requires both the visible 409 toast feedback and the cost-bound (exactly once per click) guarantee under real LLM latency."
  - test: "PII echo lands in cheap LLM response"
    expected: "Trigger a chat where the assistant reply echoes a phone or email. The titler's post-hoc regex rejects the title, the chat sidebar settles to 'Untitled chat 26 апреля' (Russian short genitive), and Loki logs contain regex_class=email/phone but no message body or matched substring."
    why_human: "Requires real cheap LLM behavior to confirm the PII echo actually surfaces — production verification cannot mock both the LLM and the rejection in the same end-to-end pass."
---

# Phase 18: Auto-Title Verification Report

**Phase Goal:** After the first assistant reply, chats auto-generate a 3-6 word title using a cheap dedicated model, background and out-of-band from the chat SSE, with atomic guards so a user's manual rename is never clobbered and no PII ever reaches logs.

**Verified:** 2026-04-26T20:00:00Z
**Status:** passed (with human-verification items for visual/timing-dependent SCs)
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths (ROADMAP Success Criteria)

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | A user opening a brand-new chat sees "Новый диалог" as the sidebar placeholder | VERIFIED | `services/frontend/app/(app)/chat/page.tsx:81` `displayTitle = conv.title === '' \|\| conv.titleStatus === 'auto_pending' ? 'Новый диалог' : conv.title`; same rule encapsulated in `components/chat/ChatHeader.tsx:41` select projection. `createConversation` mutationFn sets the placeholder at `page.tsx:185`. |
| 2 | After first assistant reply, sidebar updates async to 3-6 word title via TITLER_MODEL without flicker | VERIFIED (structural) + HUMAN (visual) | `services/api/internal/service/titler.go:113` builds `llm.ChatRequest{Model: t.model}` with system prompt `"Сформулируй короткий заголовок (3–6 слов)"` (line 41). Out-of-band propagation via `services/frontend/hooks/useChat.ts:250` `if (event.type === 'done') queryClient.invalidateQueries({queryKey: ['conversations']})`. Flicker mitigation via memoized `ChatHeader.tsx:58 export const ChatHeader = memo(ChatHeaderImpl)` + select projection at line 36. Visual smoothness deferred to human verification. |
| 3 | A user who renames a chat sees the rename persist permanently — auto-title job becomes no-op the moment rename lands | VERIFIED | `handler/conversation.go:327` `conversation.TitleStatus = domain.TitleStatusManual` (D-06 unconditional flip). `repository/conversation.go:91` `Update` $set includes `"title_status": conv.TitleStatus` (Landmine 7 fix). Atomic guard: `repository/conversation.go:155-177 UpdateTitleIfPending` filter `{_id, title_status: {$in: [TitleStatusAutoPending, nil]}}` — manual rename mid-flight matches zero docs, returns ErrConversationNotFound; `service/titler.go:208` logs InfoContext `outcome=manual_won_race` and returns. |
| 4 | A user who picks "Regenerate title" sees the job re-run exactly once; failures degrade silently | VERIFIED | Route at `router.go:122 r.Post("/conversations/{id}/regenerate-title", handlers.Titler.RegenerateTitle)`. Atomic transition `repository/conversation.go:186-207 TransitionToAutoPending` filter `{_id, title_status: {$in: [TitleStatusAuto, nil]}}` — manual or in-flight rejected. Handler `handler/titler.go:108-122` returns 409 with verbatim Russian D-02/D-03 copy; `handler/titler.go:163-167` spawns titler with detached 30s ctx. Silent-failure path: `service/titler.go:135-145` LLM error → log + recordAttempt + return (status stays auto_pending; D-04 retry on next turn). Frontend: `chat/page.tsx:204-216 regenerateTitle` mutation onError toasts the server's verbatim Russian message. |
| 5 | PII titles fall back to "Untitled chat <date>"; logs contain only metadata | VERIFIED | Pre-redact: `service/titler.go:117-118 security.RedactPII(userMsg)` and `RedactPII(assistantMsg)` before constructing ChatRequest. Post-hoc gate: `service/titler.go:164 security.ContainsPIIClass(title)` → terminal write at line 165 `untitledChatRussian(time.Now())` ("Untitled chat 26 апреля" Russian short form, lines 267-273). Log shape: every `slog.WarnContext`/`InfoContext` carries only `{conversation_id, business_id, prompt_length, response_length, rejected_by, regex_class, outcome, duration_ms}` — never message body or generated title. `TestGenerateAndSave_LogShape` (titler_test.go:223-257) asserts via `bytes.Buffer` capture that NO banned PII substring AND NO original chat content AND NO generated title appears in logs. |

**Score:** 5/5 ROADMAP success criteria verified (3 fully automated, 2 structural-verified with visual/timing items routed to human verification).

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `pkg/security/pii.go` | RedactPII / ContainsPII / ContainsPIIClass + 6 named regex classes + Luhn | VERIFIED | All three functions exported with exact signatures (lines 78, 93, 103). Six classes registered in `piiClasses` slice (line 64): email, phone, iban, passport, inn, cc. `luhnValid` at line 125. `redactionToken = "[Скрыто]"` at line 13. |
| `pkg/security/pii_test.go` | Table-driven true-positive + Russian false-positive corpus + log-shape regression | VERIFIED | TestContainsPII (line 15) — 21 cases including all 6 classes' true positives + 11 Russian false positives (Заказ 12345, Чек 9876543, Звонок 2026-04-15, Заявка 7654321098, Артикул 123456789, Счёт 1234567890123, Доход 2025, Платёж 100500, Стол 5, Отчёт 2025, Идентификатор 1234567890123456 Luhn-fail). TestRedactPII at line 67. TestRedactPII_LogShape at line 101 (Landmine 6). |
| `services/api/internal/config/config.go` | LLMModel, LLMTier, TitlerModel, OpenRouter/OpenAI/Anthropic keys, SelfHostedEndpoints | VERIFIED | 7 new fields at lines 81-95 with TITLER_MODEL→LLM_MODEL fallback at line 158-161. Graceful disable preserved (no validation at lines 167-179). |
| `services/api/internal/repository/conversation.go` | UpdateTitleIfPending, TransitionToAutoPending, EnsureConversationIndexes, Update $set with title_status | VERIFIED | UpdateTitleIfPending at line 155 with `$in: [domain.TitleStatusAutoPending, nil]` (line 159). TransitionToAutoPending at line 186 with `$in: [domain.TitleStatusAuto, nil]` (line 190). EnsureConversationIndexes at line 224 creates compound index `conversations_user_biz_title_status` (line 233). Update method line 91 `"title_status": conv.TitleStatus` (Landmine 7 / D-06 plumbing). |
| `services/api/internal/service/titler.go` | Titler struct + chatCaller seam + GenerateAndSave composing pkg/security + UpdateTitleIfPending | VERIFIED | chatCaller interface at line 58 (canonical mocking seam, B-02). Titler struct at line 67 with concrete *llm.Router via implicit structural typing. NewTitler at line 80 panics on nil router/repo/empty model. GenerateAndSave at line 113 — full pipeline (pre-redact → ChatRequest with no Tools → sanitize → ContainsPIIClass → UpdateTitleIfPending). untitledChatRussian at line 267 with [12]string genitive table. |
| `services/api/internal/service/titler_metrics.go` | Prometheus auto_title_attempts_total CounterVec + recordAttempt | VERIFIED | autoTitleAttempts CounterVec at line 23 with labels {status, outcome}. recordAttempt at line 31. No "started" / in-progress sentinel (I-02). |
| `services/api/internal/service/titler_test.go` | NilGuards table + 7 outcome-branch tests + LogShape negative regression + PreRedact | VERIFIED | TestNewTitler_NilGuards at line 76 (3 cases). TestGenerateAndSave_Success/_LLMError/_EmptyResponse/_PIIReject_Terminal/_ManualWonRace/_PersistError/_LogShape/_PreRedact at lines 107, 127, 141, 155, 183, 205, 223, 262. fakeRouter (chatCaller) and fakeConvRepo with embedded-nil interface (W-04) at lines 20-58. |
| `services/api/internal/handler/titler.go` | TitlerHandler with RegenerateTitle 7-step state machine + concrete *service.Titler | VERIFIED | TitlerHandler at line 29 with `titler *service.Titler` (concrete, B-02). RegenerateTitle at line 75. Steps: 401 unauthorized (line 78), 404 not-found (line 86), 500 lookup err (line 90), 403 forbidden (line 94), 503 titler-disabled (line 100), 409 manual D-02 verbatim Russian (line 111), 409 in-flight D-03 verbatim (line 119), atomic transition (line 128), 409 race (line 130), spawn with detached 30s ctx (line 163), 200 (line 173). |
| `services/api/internal/handler/conversation.go` | UpdateConversation flips TitleStatusManual unconditionally | VERIFIED | Line 327 `conversation.TitleStatus = domain.TitleStatusManual // Phase 18 / D-06: PUT title is unconditional manual rename` |
| `services/api/internal/handler/chat_proxy.go` | titler field + fireAutoTitleIfPending + Resume variant; fire-points within line ranges | VERIFIED | titler field at line 90 (`*service.Titler`). Constructor param at line 114. Fire-point #1 at line 609 (within 580-620 range). Fire-point #2 at line 923 (within 895-925 range). fireAutoTitleIfPending at line 971 with re-read after persist (line 982 GetByID), gate on TitleStatusAutoPending (line 988), detached 30s spawn ctx (line 995). fireAutoTitleIfPendingResume at line 1010 (same disciplines). |
| `services/api/internal/service/titler_testhelper.go` | Exported FakeChatCaller satisfying chatCaller | VERIFIED | FakeChatCaller at line 25 with Chat/LastReq/Calls accessors. Used by handler tests at `titler_test.go:121-127`. Single canonical mocking seam (no parallel titlerCaller). |
| `services/api/cmd/main.go` | EnsureConversationIndexes call + service.NewTitler + handler.NewTitlerHandler + ChatProxyHandler titler arg | VERIFIED | Line 125 EnsureConversationIndexes call. Line 198 `var llmRouter *llm.Router`. Line 220-221 `if llmRouter != nil { titler = service.NewTitler(...) }`. Line 227 `titlerHandler := handler.NewTitlerHandler(titler, conversationRepo, messageRepo)`. Line 358 ChatProxyHandler constructor receives `titler` as last arg. |
| `services/frontend/components/chat/ChatHeader.tsx` | Memoized isolated subtree subscribed via select projection | VERIFIED | 'use client' (line 1). useQuery with `select: (list) => ...` returning primitive string (line 36). Encapsulates D-09 fallback at line 41. `export const ChatHeader = memo(ChatHeaderImpl)` at line 58 (D-11 Landmine 1 mitigation). |
| `services/frontend/components/chat/ChatHeader.isolation.test.tsx` | vi.fn() + Profiler.onRender + toHaveBeenCalledTimes(1) | VERIFIED | 5 cases. vi.fn() spy at lines 82, 118, 162. React.Profiler wrapping at lines 84-86. toHaveBeenCalledTimes(1) at lines 91, 112, 125, 169 — including positive-control title-change tests asserting 2 commits, proving harness sensitivity. |
| `services/frontend/app/(app)/chat/page.tsx` | titleStatus interface + 'Новый диалог' fallback + 'Обновить заголовок' menu item + regenerateTitle mutation | VERIFIED | titleStatus optional union at line 37. displayTitle fallback at line 81. RefreshCw import at line 8. DropdownMenuItem `'Обновить заголовок'` at line 151 wrapped in `conv.titleStatus !== 'manual'` predicate (line 143). regenerateTitle mutation at line 204-216 with `api.post('/conversations/${id}/regenerate-title')` and toast.error on AxiosError. |
| `services/frontend/hooks/useChat.ts` | useQueryClient + invalidateQueries on SSE 'done' | VERIFIED | useQueryClient import at line 3. queryClient instance at line 165. handleSSEEvent invalidation branch at line 250-252 with `event.type === 'done'`. PITFALLS §13 comment at line 248. queryClient added to useCallback deps at line 280. |
| `services/frontend/hooks/__tests__/useChat.invalidate.test.ts` | Fetch-stream mock asserting exactly-1 invalidation on done | VERIFIED | mockSSEResponse + sseLine imports at line 8 (no test-only export, W-05). Two cases: invalidates exactly once on done; does not invalidate when stream lacks done. |
| `services/frontend/app/(app)/chat/__tests__/RegenerateMenuItem.test.tsx` | B-04 verbatim 409 Russian copy assertions via findByText | VERIFIED | findByText assertions for both verbatim Russian strings at lines 215, 266. Stubbed 409 responses with locked Russian bodies at lines 169, 229. Real <Toaster/> rendered for sonner toast surfacing. |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|----|--------|---------|
| chat_proxy.go auto/done persist (line 609) | titler.GenerateAndSave goroutine | fireAutoTitleIfPending → re-read GetByID → gate auto_pending → detached 30s ctx | WIRED | All 4 hops verified in source; integration test TestFireAutoTitleIfPending (chat_proxy_test.go:1240) covers gate matrix (auto_pending fires; manual/auto/titler-nil/GetByID-error all no-op). |
| chat_proxy.go streamResume done (line 923) | titler.GenerateAndSave goroutine | fireAutoTitleIfPendingResume → GetByID → ListByConversationID → spawn | WIRED | TestFireAutoTitleIfPendingResume (chat_proxy_test.go:1355) covers resume-path subcases. |
| TitlerHandler.RegenerateTitle | titler.GenerateAndSave goroutine | TransitionToAutoPending (atomic) → ListByConversationID → detached 30s ctx | WIRED | 10 RegenerateTitle test functions at handler/titler_test.go cover all branches (200, 409 manual, 409 in-flight, 409 transition-race, 503 disabled, 403 forbidden, 404 not-found, 401 unauthorized, byte-exact verbatim copy, 500 transition error). |
| Titler.GenerateAndSave | conversationRepo.UpdateTitleIfPending | atomic $in [auto_pending, null] filter | WIRED | Two call sites at titler.go:177 (terminal pii-reject path) and titler.go:207 (success path). Repository test TestUpdateTitleIfPending covers the manual-won-race no-op. |
| frontend useChat handleSSEEvent done | React Query cache invalidation | queryClient.invalidateQueries(['conversations']) | WIRED | Line 250-252; W-05 EventSource-mock test confirms exactly-1 invalidation. |
| ChatHeader (memoized) | conversation title cache | useQuery with select projection returning primitive string | WIRED | B-06 isolation test asserts no re-render on unrelated cache mutation; positive-control proves harness sensitivity. |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|---------------|--------|--------------------|----|
| ChatHeader.tsx | title (string) | useQuery['conversations'] select | Real data when API returns conversations; placeholder fallback when title=='' or auto_pending | FLOWING |
| ConversationItem (chat/page.tsx) | displayTitle | conv.title / conv.titleStatus from React Query cache | Real conversation list from `api.get('/conversations')` | FLOWING |
| Titler.GenerateAndSave | title (string) | t.router.Chat (cheap LLM via *llm.Router) | Real LLM response when wired in production; fakeRouter in tests | FLOWING |
| TitlerHandler.RegenerateTitle | userText/assistantText | h.messageRepo.ListByConversationID | Real Mongo query returning recent messages | FLOWING |
| chat_proxy fireAutoTitleIfPending | title_status | h.conversationRepo.GetByID (re-read AFTER persist) | Real Mongo doc fetched after messageRepo.Create returns | FLOWING |
| Untitled chat fallback | terminalTitle | untitledChatRussian(time.Now()) lookup table | Deterministic Russian month string from [12]string table | FLOWING |

All artifacts that render or persist dynamic data have a verified upstream source.

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| pkg/security tests pass | `cd pkg && GOWORK=off go test ./security/... -count=1` | `ok github.com/f1xgun/onevoice/pkg/security 0.914s` | PASS |
| Titler service + handler tests pass | `cd services/api && GOWORK=off go test ./internal/service/... ./internal/handler/... -count=1` | `ok services/api/internal/service 7.324s` + `ok services/api/internal/handler 0.598s` | PASS |
| Conversation repo tests (per orchestrator confirmation) | (orchestrator pre-spawn) | All 15 sub-tests pass against live Mongo | PASS |
| Frontend vitest suite (per orchestrator confirmation) | `pnpm exec vitest run` | 246 passed, 1 skipped | PASS |
| pkg/security build clean | `go build ./pkg/security/...` (transitively via test) | clean | PASS |
| services/api build clean | `go build ./services/api/...` (transitively via test) | clean | PASS |

### Requirements Coverage

| Requirement | Source Plan(s) | Description | Status | Evidence |
|-------------|---------------|-------------|--------|----------|
| TITLE-01 | 18-06 | "Новый диалог" placeholder in sidebar until generated/manual | SATISFIED | `chat/page.tsx:81` displayTitle fallback; `ChatHeader.tsx:41` select projection encapsulates same rule. ConversationItem.placeholder.test.tsx — 5 cases green. |
| TITLE-02 | 18-02, 18-04, 18-05 | Async background job uses TITLER_MODEL (cheap, env-configurable) independent of LLM_MODEL | SATISFIED | `config.go:158-161` TITLER_MODEL with LLM_MODEL fallback. `cmd/main.go:198-221` graceful Router construction. `service/titler.go:113` builds ChatRequest{Model: t.model}. |
| TITLE-03 | 18-03, 18-05 | Manual rename sets title_status="manual"; no auto job overwrites | SATISFIED | `handler/conversation.go:327` unconditional flip. `repository/conversation.go:91` Update $set persists title_status (Landmine 7). TestUpdateConversation_TitleStatusManual + _FromAutoPending guard. |
| TITLE-04 | 18-03 | Atomic conditional update on `title_status ∈ {null, auto_pending}` | SATISFIED | `repository/conversation.go:155-177 UpdateTitleIfPending` with `$in: [TitleStatusAutoPending, nil]`. TestUpdateTitleIfPending integration test covers manual-won-race no-op. |
| TITLE-05 | 18-04, 18-05 | Title job never blocks chat SSE; failures degrade silently | SATISFIED | `service/titler.go` is invoked via goroutine spawn at `chat_proxy.go:996, 1043` and `handler/titler.go:167`. Every error branch returns without status change (D-04 retry on next turn). Detached 30s spawnCtx — never r.Context(). |
| TITLE-06 | 18-06 | Out-of-band title propagation via React Query invalidation, no SSE muxing | SATISFIED | `useChat.ts:250-252` invalidate on SSE 'done'; PITFALLS §13 comment cites the rule. ChatHeader memoized + select projection prevents flicker. W-05 fetch-stream mock test asserts exactly-1 invalidation. |
| TITLE-07 | 18-04 | Logs metadata only — never prompt or response body | SATISFIED | All slog calls in `service/titler.go` carry only `{conversation_id, business_id, prompt_length, response_length, rejected_by, regex_class, outcome, duration_ms}`. TestGenerateAndSave_LogShape (titler_test.go:223) asserts NONE of 6 banned substrings + original chat content + generated title appear in captured logs. |
| TITLE-08 | 18-01, 18-04 | Regex sanitize CC/phone/email; failing title falls back to "Untitled chat <date>" | SATISFIED | `pkg/security/pii.go` 6-class regex set with Luhn-gated CC + Cyrillic-anchored passport/INN. Pre-redact at `titler.go:117-118`; post-hoc gate at `titler.go:164`; terminal fallback `untitledChatRussian` at line 267. False-positive corpus protects 21 Russian numeric titles. |
| TITLE-09 | 18-05, 18-06 | "Regenerate title" context menu resets to auto_pending and re-runs once | SATISFIED | Backend: `handler/titler.go:75 RegenerateTitle` with 7-step state machine + atomic TransitionToAutoPending serialization. Frontend: `chat/page.tsx:151 'Обновить заголовок'` menu item hidden when manual. 6 RegenerateTitle vitest cases + 10 backend handler tests. B-04 verbatim Russian copy assertion. |

All 9 TITLE requirements satisfied. No orphaned requirements (REQUIREMENTS.md confirms TITLE-01..09 all map to Phase 18).

### Decision Coverage

| Decision | Description | Status | Evidence |
|----------|-------------|--------|----------|
| D-01 | Trigger gate fires from chat_proxy.go after auto/done assistant persist | VERIFIED | `chat_proxy.go:609` (auto/done) + `chat_proxy.go:923` (resume done). fireAutoTitleIfPending re-reads conv and gates on TitleStatusAutoPending (line 988). |
| D-02 | Regenerate vs manual rename → 409 with verbatim Russian | VERIFIED | `handler/titler.go:107-114` 409 with `"Нельзя регенерировать — вы уже переименовали чат вручную"`. Frontend hides menu item when status === 'manual' (`chat/page.tsx:143`). |
| D-03 | Concurrent regenerate (in-flight) → 409 with verbatim Russian | VERIFIED | `handler/titler.go:115-122` 409 with `"Заголовок уже генерируется"`. |
| D-04 | Failure & retry policy: title-job failures leave status auto_pending unchanged | VERIFIED | `service/titler.go:135-145` LLM error path returns without repo touch. Trigger gate re-fires on next complete turn (D-01). |
| D-05 | PII regex rejection is terminal: writes "Untitled chat <date>" + flips to "auto" | VERIFIED | `service/titler.go:165 untitledChatRussian(time.Now())` + UpdateTitleIfPending writes `title_status: TitleStatusAuto` (atomic). Trigger gate stops firing because status is no longer auto_pending. |
| D-06 | PUT /conversations/{id} sets title_status="manual" unconditionally | VERIFIED | `handler/conversation.go:327` unconditional flip. `repository/conversation.go:91` Update $set persists title_status (Landmine 7). |
| D-07 | Regenerate endpoint shape: POST, no body, 200/409 | VERIFIED | Route at `router.go:122`. Handler at `handler/titler.go:75-174`. 200 (no body) at line 173. 409 at lines 109, 117, 130. |
| D-08 | Success path atomic conditional update via UpdateTitleIfPending | VERIFIED | `repository/conversation.go:155-177` with `$in: [TitleStatusAutoPending, nil]` filter. `service/titler.go:177, 207` two call sites. |
| D-08a | Mongo compound index {user_id, business_id, title_status} idempotent | VERIFIED | `repository/conversation.go:224-243 EnsureConversationIndexes` with named index `conversations_user_biz_title_status`. Wired at `cmd/main.go:125`. TestEnsureConversationIndexes_Idempotent guards. |
| D-09 | Pending placeholder text "Новый диалог" verbatim | VERIFIED | `chat/page.tsx:81` displayTitle fallback; `ChatHeader.tsx:41` select projection. No shimmer/skeleton. |
| D-10 | Frontend useChat invalidates ['conversations'] exactly once on SSE 'done' | VERIFIED | `useChat.ts:250-252`; PITFALLS §13 comment. W-05 fetch-stream mock test asserts exactly 1 invalidation. |
| D-11 | USER OVERRIDE — header live-updates with sidebar; structural mitigation mandatory | VERIFIED | `ChatHeader.tsx:58 export const ChatHeader = memo(ChatHeaderImpl)` + select projection at line 36 returns primitive string. ChatWindow.tsx renders ChatHeader as sibling of message list and composer (lines 101-107). B-06 isolation test (toHaveBeenCalledTimes(1) after unrelated mutation) + positive-control proves harness sensitivity. |
| D-12 | Regenerate UI placement: sidebar context menu only; hidden on manual | VERIFIED | `chat/page.tsx:143` `{conv.titleStatus !== 'manual' && (...)}`. Position between Переименовать and Удалить (line 140-153). |
| D-13 | Broader PII regex set including IBAN, RU passport, INN with Russian false-positive corpus | VERIFIED | 6 classes in `pkg/security/pii.go:64`. False-positive corpus in pii_test.go:35-46 covers 11 Russian titles. INN/passport require explicit prefix anchors (Cyrillic ИНН/паспорт or strict 4+6 whitespace). |
| D-14 | Defense-in-depth pre-redact: cheap LLM never sees raw PII | VERIFIED | `service/titler.go:117-118` `security.RedactPII(userMsg)` and `RedactPII(assistantMsg)` BEFORE constructing ChatRequest. TestGenerateAndSave_PreRedact (titler_test.go:262) asserts request body contains `[Скрыто]` and not raw PII. |
| D-15 | Helper module home: pkg/security/pii.go reusable | VERIFIED | Package-level pure functions at `pkg/security/pii.go`. No struct, no constructor. Imported by services/api/internal/service/titler.go. Phase 19 ready. |
| D-16 | Failure log shape: structured fields only with regex_class, never matched substring | VERIFIED | All slog calls in titler.go carry only the documented metadata fields. ContainsPIIClass returns class name (string) only — never the matched substring. TestGenerateAndSave_LogShape (titler_test.go:223) negative-asserts NO PII byte appears in captured logs. |

All 16 LOCKED decisions plus D-08a are verified. **No deviations found.**

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| (none) | — | — | — | None observed in Phase 18 surface |

### Landmine / Pitfall Verification

| Landmine | Mitigation | Status |
|----------|-----------|--------|
| Landmine 1 (D-11 flicker) | Memo + select projection + sibling structure | VERIFIED via ChatHeader.isolation.test.tsx |
| Landmine 2 (passport/INN false-positives) | Cyrillic prefix anchors + Luhn for cc | VERIFIED via 11-case Russian corpus |
| Landmine 3 (services/api missing pkg/llm import) | First import landed; go.sum refreshed | VERIFIED via `cmd/main.go` imports + clean build |
| Landmine 4 (re-read after persist, not before) | fireAutoTitleIfPending GetByID AFTER messageRepo.Create | VERIFIED at chat_proxy.go:982 |
| Landmine 5 (r.Context() cancels at SSE close) | context.WithTimeout(context.Background(), 30*time.Second) | VERIFIED at chat_proxy.go:995, 1042 + handler/titler.go:163 |
| Landmine 6 (negative log assertion) | TestRedactPII_LogShape (pii_test.go:101) + TestGenerateAndSave_LogShape (titler_test.go:223) | VERIFIED — both use bytes.Buffer + strings.Contains negative assertion |
| Landmine 7 (D-06 plumbing in Update) | $set includes "title_status": conv.TitleStatus | VERIFIED at repository/conversation.go:91 |
| Landmine 8 (no omitempty on TitleStatus bson tag) | Bson tag at mongo_models.go:45 has no omitempty | VERIFIED via grep — 0 matches for "TitleStatus.*omitempty" |
| Pitfall 1 (graceful disable on missing env) | API boots cleanly with no LLM env; titler stays nil | VERIFIED at cmd/main.go:198-225 |

### Bonus Quality Gates

| Gate | Status | Evidence |
|------|--------|----------|
| B-02 (single mocking seam — no parallel titlerCaller) | VERIFIED | `grep "type titlerCaller interface"` returns 0 matches across all phase files; handler tests use service.FakeChatCaller directly |
| B-03 (NewTitler nil-guard table) | VERIFIED | TestNewTitler_NilGuards covers nil router, nil repo, empty model with recover()-based panic-message assertion |
| B-04 (verbatim 409 Russian copy via toast findByText) | VERIFIED | RegenerateMenuItem.test.tsx:215, 266 — findByText assertions on locked Russian strings |
| B-05 (fire-point line-range guards) | VERIFIED | awk 'NR>=580 && NR<=620' returns line 609; awk 'NR>=895 && NR<=925' returns line 923 |
| B-06 (vi.fn() + Profiler.onRender + toHaveBeenCalledTimes(1)) | VERIFIED | ChatHeader.isolation.test.tsx — 5 cases including positive-control proving harness sensitivity |
| W-04 (fakeConvRepo embedded-nil interface) | VERIFIED | titler_test.go:46-51 with comment citing W-04 resolution |
| W-05 (no _handleSSEEventForTest) | VERIFIED | grep returns 0 matches in useChat.ts and useChat.invalidate.test.ts; mockSSEResponse used instead |
| I-02 (no in-progress Prometheus sentinel) | VERIFIED | grep "started" in titler_metrics.go returns 0 matches |

### Human Verification Required

The structural mitigations for SC-2, SC-3, SC-4, SC-5 are all in place and verified by automated tests, but four end-to-end behaviors depend on real LLM round-trip timing or visual UX qualities that automated tests cannot prove offline. See the `human_verification:` block in frontmatter for the four scripted UAT items:

1. **End-to-end smoke (SC-1, SC-2)** — fresh chat → assistant reply → sidebar updates 3-6 word RU title without flicker, no composer focus loss, no scroll jump
2. **Manual rename mid-flight (SC-3)** — race-window proof of "manual sovereign" against real cheap LLM latency
3. **Regenerate exactly once + 409 toast (SC-4)** — visible 409 toast feedback under real latency
4. **PII echo terminal fallback (SC-5)** — confirm regex_class metadata in Loki + sidebar shows "Untitled chat 26 апреля"

These items are scripted in the frontmatter for the developer's UAT pass.

### Gaps Summary

**No gaps found.** All 5 ROADMAP success criteria, all 9 TITLE requirements, and all 16+ LOCKED decisions are verified by source code references and passing automated tests. The 4 human-verification items are about visual/timing qualities that the structural code already addresses; they are routine UAT items rather than gaps in the implementation.

The implementation also added bonus quality gates (B-02 through B-06, I-02, W-04, W-05) that provide stronger guarantees than the original PLAN required:

- B-02: single canonical mocking seam (chatCaller) reused transitively by handler tests
- B-05: awk line-range guards on the chat_proxy fire-point call sites
- B-06: positive-control test in ChatHeader isolation proves harness sensitivity
- I-02: dropped phantom in-progress Prometheus label that would never increment
- W-04: embedded-nil interface fakeConvRepo louder failure mode
- W-05: fetch-stream mock instead of test-only export — production SSE path tested end-to-end

---

_Verified: 2026-04-26T20:00:00Z_
_Verifier: Claude (gsd-verifier)_
