# Phase 18: Auto-Title — Research

**Researched:** 2026-04-26
**Domain:** Backend async fire-and-forget LLM titling job + atomic Mongo conditional update + frontend React Query invalidation + reusable PII sanitization helper
**Confidence:** HIGH

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**Trigger & Regenerate Flow**

- **D-01: Trigger gate.** The auto-title job fires from `chat_proxy.go` when, after the auto/done assistant-message persist (currently around `chat_proxy.go:578–593`), the conversation's `title_status == "auto_pending"` AND the just-persisted assistant message has `Status == "complete"`. HITL pauses (`Status == "pending_approval"`) do NOT trigger; once the user resumes and the turn produces a complete assistant message via the resume path (`chat_proxy.go:streamResume "done" branch ~line 906`), that counts as a triggering completion. The trigger is NOT scoped to "first complete assistant message only" — see D-04 for the retry rationale.
- **D-02: Regenerate vs manual rename.** `POST /api/v1/conversations/{id}/regenerate-title` returns **409 Conflict** with a Russian error body (`"Нельзя регенерировать — вы уже переименовали чат вручную"`) when `title_status == "manual"`. Frontend hides the menu item entirely when `title_status === 'manual'` to avoid even surfacing the action. Manual rename is sovereign — this rule is the trust-critical contract from PITFALLS §12.
- **D-03: Concurrent regenerate (in-flight job).** Server returns **409 Conflict** with body `"Заголовок уже генерируется"` when the user clicks Regenerate while `title_status == "auto_pending"` (a job is in flight or about to be). Frontend toasts the message. Note: this overrides the typical "idempotent no-op" pattern — user explicitly asked for visible state feedback.
- **D-04: Failure & retry policy.** A title-job failure (LLM error, JSON parse failure, network timeout) leaves `title_status` unchanged at `"auto_pending"`. The trigger gate (D-01) re-fires on every subsequent COMPLETE assistant turn while status remains `auto_pending`. Self-healing for transient failures. The atomic conditional update on the success path (D-08) prevents double-write. There is NO attempt counter; cost is bounded by user-message volume in single-owner v1.3.
- **D-05: PII regex rejection is terminal.** When the post-hoc PII regex matches the generated title (D-09), the title job sets `title = "Untitled chat 26 апреля"` (date in Russian short form) and `title_status = "auto"`. Status flips to terminal — the trigger gate stops firing because it requires `auto_pending`. The chat keeps the date stamp until the user renames or clicks Regenerate. Distinguishes from D-04 because chat content reliably keeps producing PII echoes; retry burns cost.
- **D-06: PUT /conversations/{id} sets `title_status = "manual"` unconditionally.** Any successful PUT with a `title` field flips the status — even if the new title equals the old one or is an empty string. Predictable, no read-before-write, no special flag. The existing frontend rename UI (`services/frontend/app/(app)/chat/page.tsx:167-171`) needs zero change. Server-side change is to the existing `UpdateConversation` handler.
- **D-07: Regenerate endpoint shape.** `POST /api/v1/conversations/{id}/regenerate-title` (POST, no body, 200 on accepted, 409 for `manual` or in-flight). Atomic transition `title_status: "auto" → "auto_pending"` (or unchanged if already `auto_pending`); fires the goroutine. Returns immediately — does not block on the LLM call. Path is action-oriented per `docs/api-design.md` for non-CRUD verbs.

**Atomic Storage Semantics**

- **D-08: Success path — atomic conditional update.** Titler writes via `conversationRepo.UpdateTitleIfPending(ctx, id, generatedTitle)` which translates to a Mongo `UpdateOne({_id, title_status: {$in: ["auto_pending", null]}}, {$set: {title, title_status: "auto", updated_at}})`. Manual renames that land mid-job match zero documents; the titler's update is a no-op. Existing `Update(ctx, conv)` repo method stays as the rename path; a new repo method handles the conditional update separately so semantics are explicit.
- **D-08a: Mongo index.** Add a compound index `{user_id: 1, business_id: 1, title_status: 1}` to keep the atomic update predicate cheap; reuses Phase 15's sidebar index direction.

**Sidebar Pending UX**

- **D-09: Pending placeholder text.** Sidebar / chat list rows render the literal `"Новый диалог"` whenever `conversation.title === ""` OR `title_status === "auto_pending"`. No shimmer, no skeleton, no animation — matches TITLE-01 verbatim. The current frontend already passes this string in `createConversation` (`chat/page.tsx:159`), so the rendering change is a fallback condition in the row component.
- **D-10: Title arrival propagation.** Frontend `useChat` hook calls `queryClient.invalidateQueries({ queryKey: ['conversations'] })` exactly once when the chat SSE emits `done`, regardless of whether a title job is running. PITFALLS §13 hard rule: **never mux title updates into the chat SSE itself.** No new SSE side channel, no polling.
- **D-11: Header behavior — live update with sidebar (USER OVERRIDE).** Chat header reads from the same React Query cache as the sidebar, so it updates the moment the title lands. This **overrides** PITFALLS §13's stable-snapshot recommendation. Planner MUST mitigate the flicker risk by ensuring the header is a self-contained React subtree that re-renders independently of the message list and composer (e.g., an isolated `<ChatHeader>` component subscribed to a memoized selector for `title` only).
- **D-12: Regenerate UI placement.** Single affordance: a `"Обновить заголовок"` item in the sidebar chat-row context menu (between `Переименовать` and `Удалить`, in `chat/page.tsx:117–141`). Hidden when `title_status === "manual"` (D-02).

**PII Sanitization**

- **D-13: Regex set (broader).** Credit card (Luhn-shaped 13–19 digit groups with optional separators), international/RU phone (E.164 + `+7 (XXX) XXX-XX-XX` + `8 XXX XXX-XX-XX`), email (RFC 5322 simple), IBAN (country code + 2 check digits + 11–30 chars), RU passport (10-digit), INN (10/12-digit). Test corpus must include legitimate Russian numeric titles that PASS.
- **D-14: Defense-in-depth — pre-redact prompt.** User's first message + first assistant reply pass through `pkg/security/pii.RedactPII` BEFORE being sent to the cheap LLM. Matches replaced with `[Скрыто]`. Cheap model never sees raw PII. Post-hoc `pkg/security/pii.ContainsPII` on the generated title is a second line of defense.
- **D-15: Helper module home.** New `pkg/security/pii.go` with `RedactPII(s string) string` and `ContainsPII(s string) bool`. Table-driven tests in `pkg/security/pii_test.go`. The titler module composes; it does not own the regex.
- **D-16: Failure log shape.** `slog.WarnContext` with `{conversation_id, business_id, prompt_length, response_length, rejected_by: "pii_regex", regex_class: "<phone|cc|email|iban|passport|inn>"}`. Carries the rule that fired but never the matched substring.

### Claude's Discretion

- TITLER_MODEL provider/fallback chain — default to `LLM_MODEL` if `TITLER_MODEL` is unset. Pick a concrete cheap model name supported by the existing `pkg/llm` Router today.
- System prompt wording: research draft `"Сформулируй короткий заголовок (3–6 слов) для этого диалога. Без кавычек и точек в конце."`
- Max output tokens (research draft 20–30) and temperature (0.3).
- Title length cap on post-LLM trim/sanitize step (research draft 60–80 chars).
- Russian date-stamp formatting for the `Untitled chat <date>` fallback (research draft `26 апреля` short form).
- Prometheus metric names: research draft `auto_title_attempts_total{status,outcome}`.
- Whether the regenerate endpoint persists a system note (most likely NOT).
- Frontend toast component to use for 409 messages.

### Deferred Ideas (OUT OF SCOPE)

- Hard cost cap with attempt counter — explicitly rejected for v1.3.
- HMAC-hashed PII-rejection log fields — rejected for single-owner deployment.
- Regenerate-title system note in chat history — deferred.
- Dedicated `/conversations/stream` SSE side channel — rejected.
- Title regen in chat header dropdown — rejected for Phase 18 (sidebar context menu only).

</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| TITLE-01 | Sidebar shows `"Новый диалог"` placeholder until generated/renamed | D-09 frontend rendering rule; placeholder string already passed at create time (`chat/page.tsx:159`); render fallback condition in `ConversationItem` row |
| TITLE-02 | After first assistant reply, async background job uses `TITLER_MODEL` env-configurable independent from `LLM_MODEL` | §"Titler service architecture" details the in-process goroutine pattern + env config; §"LLM Router model override" documents the per-call `ChatRequest.Model` already supports it |
| TITLE-03 | Manual rename sets `title_status: "manual"` and is sovereign | D-06: PUT handler unconditionally sets manual; §"PUT /conversations handler change" shows the one-line patch |
| TITLE-04 | Atomic conditional update guards manual renames mid-flight | D-08: `UpdateTitleIfPending` repo method using Mongo `UpdateOne` with `title_status ∈ {"auto_pending", null}` filter; §"Atomic conditional update idiom" |
| TITLE-05 | Title job never blocks chat SSE; failures degrade silently | §"Trigger fire-points" + §"persistCtx pattern": goroutine spawned with detached context, request ctx may cancel without affecting titler; failures log + leave status unchanged (D-04) |
| TITLE-06 | Out-of-band propagation via on-navigation refetch (no flicker / focus loss) | D-10: `queryClient.invalidateQueries(['conversations'])` on SSE `done`; D-11 + §"Isolated ChatHeader subtree" structural recipe |
| TITLE-07 | Logs metadata only — no prompt body, no response content | D-16 + §"Logging shape": fields `{conversation_id, business_id, prompt_length, response_length, rejected_by, regex_class}` only; never matched substring |
| TITLE-08 | Server-side regex sanitization rejects CC/phone/email/IBAN/passport/INN; falls back to `"Untitled chat <date>"` | D-13 + D-15: `pkg/security/pii.go` regex set; §"Russian regex patterns" + §"False-positive corpus" |
| TITLE-09 | `POST /conversations/{id}/regenerate-title` resets to `auto_pending` and re-runs once | D-07: handler shape, 200/409 contract; §"Regenerate endpoint routing" |

</phase_requirements>

## Project Constraints (from CLAUDE.md / AGENTS.md)

**Repo-wide**

- **NO `Co-Authored-By:` lines in commit messages.** User preference; Phase 18 commits must follow.
- Commit format: `<type>: <subject>` (feat, fix, refactor, docs, test, chore, ci) — applies to Phase 18 commits.
- `.planning/` is in `.gitignore` but GSD artifacts inside it are committed via `git add -f`. Plain `git add` errors on new files there.
- `make lint-all` and `make test-all` must pass before merge.

**`pkg/` (shared)**

- Only shared code goes in `pkg/`. PII module is a perfect fit (`pkg/security/`) because Phase 19 search-query logging will reuse it.
- Repository interfaces live in `pkg/domain/repository.go`; implementations in each service.
- Sentinel errors: `var ErrXxx = errors.New("...")` in `pkg/domain/errors.go`.

**`services/api/`**

- Layered: Handler → Service → Repository (no skipping).
- Handlers parse HTTP, call service, format response. No DB access.
- Services: business logic, validation, error wrapping. No HTTP concepts.
- Repositories: SQL/Mongo queries only. Convert DB errors to domain errors.
- Errors wrapped with `fmt.Errorf("context: %w", err)`.
- DB migrations: dual-path rule does NOT apply for Phase 18 (no SQL migrations; the new Mongo index is created programmatically at API startup, like the existing Phase 15 / 16 indexes).
- `t.Setenv` (not `os.Setenv`) in tests.

**`services/frontend/`**

- Server components by default; `'use client'` only when using hooks/events.
- Tailwind only (no inline styles, no CSS modules).
- Forms: react-hook-form + zod.
- State: Zustand global, React Query server, `useState` local UI only.
- Components: `function` declarations (not arrow), typed props interfaces.
- `import type { ... }` for type-only imports.

**Security (`docs/security.md`)**

- MUST NEVER log secrets / PII. Auto-title goes onto the logging-exemption list.
- Validate all external input at the handler boundary.
- Parameterized queries only (BSON for Mongo); no `fmt.Sprintf` for queries.

## Summary

Phase 18 grafts a fire-and-forget auto-titler onto the existing API service with **zero changes to the orchestrator**, **zero new SQL migrations**, and **one tiny frontend addition**. The architectural shape is small: a new `services/api/internal/service/titler.go` (~150 LOC), a new `pkg/security/pii.go` (~80 LOC + table-driven tests), one new repo method (`UpdateTitleIfPending`), one new HTTP handler (`POST /conversations/{id}/regenerate-title`), one new Mongo index added to the existing API-startup index block, two surgical edits in `chat_proxy.go` (auto/done persist site at line 578–593 and the resume `done` branch at line 906), one one-line edit in `UpdateConversation` (PUT handler), and a single React Query invalidation call inside `useChat.ts` plus a new `<DropdownMenuItem>` in `chat/page.tsx`.

The trust-critical primitive is `domain.ConversationRepository.UpdateTitleIfPending(ctx, id, title)` — a Mongo `UpdateOne` with filter `{_id, title_status: {$in: ["auto_pending", null]}}`. If the user renames mid-job, the rename sets `title_status="manual"`, the filter matches zero documents, and the titler's write becomes a silent no-op. Manual renames are sovereign by construction. The post-hoc PII gate (D-15: `pkg/security/pii.ContainsPII`) is a second line of defense that flips terminal `auto` with the date-stamped fallback.

The most important architectural fact this research uncovered (and the planner MUST honor): **`services/api` does NOT currently import `pkg/llm`**. Confirmed via `grep -r github.com/f1xgun/onevoice/pkg/llm services/api` returning zero hits. The Router lives in `services/orchestrator/cmd/main.go`. To follow ARCHITECTURE.md §5.2's recommendation ("In-process goroutine inside API service"), Phase 18 must wire a *second* `llm.Router` instance into `services/api/cmd/main.go` (with the same `pkg/llm/providers` setup as orchestrator). This is a non-trivial wiring change — see §"Titler service architecture" below for the concrete pattern.

**Primary recommendation:** Build a self-contained `service.Titler` in the API service with its own thin `llm.Router` initialized from API-side config (`TITLER_MODEL`, `OPENROUTER_API_KEY`, etc.). Compose `pkg/security/pii.RedactPII` (input) and `pkg/security/pii.ContainsPII` (output). Wire it into `chat_proxy.go` at exactly two fire-points (lines 578–593 and 906) via a single helper, and into the new `regenerate-title` handler. Atomic conditional update guarantees correctness — no orchestrator changes needed.

## Standard Stack

### Core (already present — reuse, do not add)

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/f1xgun/onevoice/pkg/llm` | (workspace) | LLM Router with per-call `ChatRequest.Model` override | Already proven; orchestrator uses it. `[VERIFIED: pkg/llm/types.go:46]` shows `ChatRequest.Model` is per-request. |
| `github.com/f1xgun/onevoice/pkg/llm/providers` | (workspace) | OpenRouter / OpenAI / Anthropic / SelfHosted adapters | Same providers used in orchestrator's `cmd/main.go` (lines 740–778) — copy that wiring pattern verbatim. `[VERIFIED: services/orchestrator/cmd/main.go:740–778]` |
| `go.mongodb.org/mongo-driver/v2` | v2.5.0 | Mongo driver — `UpdateOne` with `$in` filter | `[VERIFIED: services/api/internal/repository/conversation.go:78–99]` already uses this driver for `Update`; pattern matches. |
| `log/slog` | stdlib | Structured logs with `WarnContext` | `[VERIFIED: services/api/internal/handler/chat_proxy.go:184]` codebase standard. |
| `github.com/prometheus/client_golang/prometheus/promauto` | v1.23.2 | Counter/histogram declaration | `[VERIFIED: pkg/metrics/llm.go:6–7]` — exact pattern to copy. |
| `github.com/go-chi/chi/v5` | v5.2.5 | HTTP router for new POST endpoint | `[VERIFIED: services/api/internal/router/router.go:7]` |
| `@tanstack/react-query` | 5.90.21 | Frontend cache invalidation | `[VERIFIED: services/frontend/app/(app)/chat/page.tsx:4]` already used. |
| `sonner` | 2.0.7 | Frontend toast | `[VERIFIED: services/frontend/hooks/useChat.ts:2]` — `import { toast } from 'sonner'` is the canonical pattern. |
| `lucide-react` | 0.564.0 | Icons (refresh icon for regenerate menu item) | `[VERIFIED: services/frontend/app/(app)/chat/page.tsx:8]` — `import { ... } from 'lucide-react'`. |

### Supporting

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `regexp` (stdlib) | — | PII pattern matching | All PII regexes; precompile in package-level `var`s. |
| `strings` (stdlib) | — | Trim, length cap, quote stripping | Title sanitization step. |
| `time` (stdlib) | — | Russian date-stamp formatting | `"26 апреля"` short form for fallback. |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Wiring a second `llm.Router` into API | API calls into orchestrator over HTTP for titling | Adds an internal API surface, an extra hop, and makes orchestrator restart break titling. Strict ARCHITECTURE.md §5.2 says titler lives in API — keep it self-contained. |
| `pkg/security/pii.go` reusable helper | Inline regexes in `service/titler.go` | Phase 19 search-query logging needs the same regex set; D-15 explicitly chose pkg/security/. Keep it. |
| Separate background worker | Inline goroutine in API process | At single-owner v1.3 scale (~20 messages/day), goroutine is correct. ARCHITECTURE.md §5.2 confirms. |
| Separate llm.Tier | `Tier="background"` to isolate billing | Discretionary — `[ASSUMED]` we can set `Tier="background"` for telemetry isolation. Confirm with planner; not a blocker. |

**No new dependencies needed.** The Go workspace already has every library this phase touches. Frontend has zero new packages.

**Version verification (`npm view` / `go list` not strictly needed — all libs are existing repo dependencies):**

```bash
# Confirm versions in go.work (no new modules)
grep -r "go.mongodb.org/mongo-driver" services/api/go.mod   # v2.5.0
grep -r "github.com/prometheus/client_golang" services/api/go.mod  # v1.23.2
# Frontend - no install needed
```

## Architecture Patterns

### Recommended File Layout

```
pkg/
├── security/
│   ├── pii.go              # NEW — RedactPII / ContainsPII
│   └── pii_test.go         # NEW — table-driven, Russian + PII corpus
└── domain/
    └── repository.go       # MODIFIED — add UpdateTitleIfPending to ConversationRepository

services/api/
├── cmd/main.go             # MODIFIED — wire llm.Router + Titler service + new index + new route
├── internal/
│   ├── config/config.go    # MODIFIED — TITLER_MODEL, LLM provider keys, LLM_MODEL fallback
│   ├── handler/
│   │   ├── conversation.go # MODIFIED — UpdateConversation flips title_status="manual" (D-06)
│   │   └── titler.go       # NEW — POST /conversations/{id}/regenerate-title
│   ├── repository/
│   │   └── conversation.go # MODIFIED — UpdateTitleIfPending impl + index creation helper
│   └── service/
│       ├── titler.go       # NEW — Titler.GenerateAndSave(ctx, convID, [userMsg, assistantMsg])
│       └── titler_test.go  # NEW — unit tests with mocked llm.Router and conversationRepo
└── chat_proxy.go           # MODIFIED — fire titler at lines 578–593 and 906 (one helper, two call sites)

services/frontend/
├── app/(app)/chat/page.tsx     # MODIFIED — add "Обновить заголовок" DropdownMenuItem (hidden if manual); render "Новый диалог" placeholder when title==="" || titleStatus==="auto_pending"
├── hooks/useChat.ts            # MODIFIED — invalidate ['conversations'] on SSE 'done'
├── components/chat/ChatHeader.tsx  # NEW (or refactored existing) — isolated subtree, memoized title selector
└── lib/api.ts                  # MODIFIED — typed mutation for regenerateTitle (optional helper)
```

### Pattern 1: Titler service architecture (the central pattern)

**What:** Self-contained `Titler` service in API that holds its own `llm.Router`, builds a `ChatRequest{Model: TITLER_MODEL, ...}`, sanitizes input via `pkg/security/pii.RedactPII`, and calls `conversationRepo.UpdateTitleIfPending` on success.

**When to use:** Both call sites in `chat_proxy.go` (auto/done at 578–593, resume done at 906) AND the new `regenerate-title` handler — all three pass through one function.

**Example:**
```go
// services/api/internal/service/titler.go
// Source: composed from existing patterns in pkg/llm/router.go (Router.Chat),
// services/api/internal/handler/chat_proxy.go:166–172 (persistCtx),
// pkg/llm/router.go:186 (go r.logBilling fire-and-forget).

package service

import (
    "context"
    "fmt"
    "log/slog"
    "strings"
    "time"

    "github.com/google/uuid"

    "github.com/f1xgun/onevoice/pkg/domain"
    "github.com/f1xgun/onevoice/pkg/llm"
    "github.com/f1xgun/onevoice/pkg/security"
)

const (
    titleMaxChars        = 80
    titleMaxOutputTokens = 30
    titleTemperature     = 0.3
    // System prompt — Russian (target audience). Cheap model instruction.
    titleSystemPrompt = "Сформулируй короткий заголовок (3–6 слов) для этого диалога. Без кавычек и точек в конце."
)

// Titler generates short titles for chats via the cheap TITLER_MODEL and
// writes them atomically via the conditional UpdateTitleIfPending repo path.
// All operations are best-effort fire-and-forget; failures degrade silently
// (D-04 / TITLE-05). The service composes pkg/security/pii so PII never
// reaches the cheap LLM endpoint or the post-hoc title check (D-13/D-14).
type Titler struct {
    router *llm.Router
    repo   domain.ConversationRepository
    model  string
}

func NewTitler(router *llm.Router, repo domain.ConversationRepository, model string) *Titler {
    if router == nil { panic("NewTitler: router cannot be nil") }
    if repo == nil { panic("NewTitler: repo cannot be nil") }
    if model == "" { panic("NewTitler: model cannot be empty (set TITLER_MODEL or LLM_MODEL)") }
    return &Titler{router: router, repo: repo, model: model}
}

// GenerateAndSave runs the full pipeline. Caller MUST pass a long-lived ctx
// (request ctx is unsafe — see chat_proxy.go's persistCtx pattern).
func (t *Titler) GenerateAndSave(ctx context.Context, businessID, conversationID, userMsg, assistantMsg string) {
    metricStart := time.Now()
    recordAttempt("started", "ok") // metric

    // Pre-redact (D-14): cheap model never sees raw PII.
    redactedUser := security.RedactPII(userMsg)
    redactedAssistant := security.RedactPII(assistantMsg)

    promptLen := len(redactedUser) + len(redactedAssistant)

    req := llm.ChatRequest{
        UserID: uuid.Nil, // system-level call, no rate-limit attribution
        Model:  t.model,
        Messages: []llm.Message{
            {Role: "system", Content: titleSystemPrompt},
            {Role: "user", Content: fmt.Sprintf("Пользователь: %s\n\nАссистент: %s", redactedUser, redactedAssistant)},
        },
        MaxTokens:   titleMaxOutputTokens,
        Temperature: titleTemperature,
        Tier:        "background",
        // NO Tools — titler must not be tool-calling.
    }

    resp, err := t.router.Chat(ctx, req)
    if err != nil {
        slog.WarnContext(ctx, "auto-title: llm error",
            "conversation_id", conversationID,
            "business_id", businessID,
            "prompt_length", promptLen,
            "rejected_by", "llm_error",
            "duration_ms", time.Since(metricStart).Milliseconds(),
        )
        recordAttempt("failure", "llm_error")
        return // D-04: status unchanged.
    }

    title := sanitizeTitle(resp.Content)
    if title == "" {
        slog.WarnContext(ctx, "auto-title: empty after sanitize",
            "conversation_id", conversationID,
            "business_id", businessID,
            "prompt_length", promptLen,
            "response_length", len(resp.Content),
            "rejected_by", "empty_response",
        )
        recordAttempt("failure", "empty_response")
        return
    }

    // D-13: post-hoc PII gate. PII match is terminal (D-05).
    if class, hit := security.ContainsPIIClass(title); hit {
        terminalTitle := untitledChatRussian(time.Now())
        slog.WarnContext(ctx, "auto-title: pii rejected",
            "conversation_id", conversationID,
            "business_id", businessID,
            "prompt_length", promptLen,
            "response_length", len(resp.Content),
            "rejected_by", "pii_regex",
            "regex_class", class,
        )
        // Write the fallback under the SAME atomic guard so a manual rename
        // mid-flight still wins.
        if err := t.repo.UpdateTitleIfPending(ctx, conversationID, terminalTitle); err != nil {
            slog.WarnContext(ctx, "auto-title: terminal write failed",
                "conversation_id", conversationID, "error", err)
            recordAttempt("failure", "terminal_write_error")
            return
        }
        recordAttempt("failure", "pii_reject")
        return
    }

    if err := t.repo.UpdateTitleIfPending(ctx, conversationID, title); err != nil {
        if errors.Is(err, domain.ErrConversationNotFound) {
            // MatchedCount=0 → user renamed (manual_won_race) or doc deleted.
            slog.InfoContext(ctx, "auto-title: no-op (manual rename or deleted)",
                "conversation_id", conversationID, "business_id", businessID,
                "response_length", len(resp.Content))
            recordAttempt("failure", "manual_won_race")
            return
        }
        slog.WarnContext(ctx, "auto-title: persist error",
            "conversation_id", conversationID, "error", err)
        recordAttempt("failure", "persist_error")
        return
    }

    slog.InfoContext(ctx, "auto-title: success",
        "conversation_id", conversationID, "business_id", businessID,
        "prompt_length", promptLen, "response_length", len(resp.Content),
        "duration_ms", time.Since(metricStart).Milliseconds())
    recordAttempt("success", "ok")
}

// sanitizeTitle strips quotes, trailing punctuation, surrounding whitespace,
// and caps length at 80 chars (D: discretionary 60–80).
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

// untitledChatRussian returns "Untitled chat 26 апреля" with the day-month in
// Russian short form using nominative-case month names from a fixed table.
func untitledChatRussian(t time.Time) string {
    months := [...]string{
        "января", "февраля", "марта", "апреля", "мая", "июня",
        "июля", "августа", "сентября", "октября", "ноября", "декабря",
    }
    return fmt.Sprintf("Untitled chat %d %s", t.Day(), months[t.Month()-1])
}
```

### Pattern 2: Atomic conditional update (the trust-critical primitive)

**What:** Mongo `UpdateOne` with `$in` filter on `title_status`. Return `domain.ErrConversationNotFound` when `MatchedCount == 0` so the titler can distinguish "no-op (race lost)" from "actual error."

**When to use:** Every titler write. Never fall back to unconditional update.

**Example:**
```go
// services/api/internal/repository/conversation.go (MODIFIED — append)
// Source: pattern from existing UpdateProjectAssignment at conversation.go:118–133.

// UpdateTitleIfPending atomically sets title + title_status="auto" only when
// title_status is currently "auto_pending" or null. A concurrent manual
// rename (PUT /conversations/{id}) flips title_status to "manual" first,
// causing this filter to match zero documents — the titler write becomes a
// silent no-op (TITLE-04 / D-08). The repo returns ErrConversationNotFound
// in that case so the caller can distinguish "manual won race" from a real
// error.
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
        return domain.ErrConversationNotFound // semantic: no eligible doc (manual won race OR deleted)
    }
    return nil
}
```

`[CITED: existing UpdateProjectAssignment at services/api/internal/repository/conversation.go:118–133]` — exact pattern to mirror.
`[CITED: MongoDB docs — $in operator]` https://www.mongodb.com/docs/manual/reference/operator/query/in/ — `$in` accepts values of any BSON type including null.

### Pattern 3: Compound index registration at API startup

**What:** Use the existing index-creation block in `services/api/cmd/main.go` as a model. The Phase 16 `EnsurePendingToolCallsIndexes` block (main.go:108–115) is the exact precedent — call a similar helper for the conversation index.

**Example:**
```go
// services/api/internal/repository/conversation.go (MODIFIED — append helper)
// Source: pattern from services/api/internal/repository/pending_tool_call.go:62–94.

// EnsureConversationIndexes creates the Phase 18 sidebar query index
// idempotently. The compound (user_id, business_id, title_status) supports
// (a) fast atomic UpdateTitleIfPending lookups for the auto-titler and
// (b) sidebar list queries that filter by user/business and surface
// auto_pending rows distinctly. Safe to call on every boot — Mongo's
// CreateMany silently succeeds when specs match existing indexes.
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
    }
    _, err := coll.Indexes().CreateMany(ctx, models)
    if err != nil {
        if mongo.IsDuplicateKeyError(err) {
            return nil
        }
        return fmt.Errorf("ensure conversation indexes: %w", err)
    }
    return nil
}
```

Wired in `cmd/main.go` immediately after `repository.EnsurePendingToolCallsIndexes` (around line 116):
```go
indexesCtx2, indexesCancel2 := context.WithTimeout(ctx, 30*time.Second)
if err := repository.EnsureConversationIndexes(indexesCtx2, mongoDB); err != nil {
    indexesCancel2()
    slog.ErrorContext(indexesCtx2, "failed to ensure conversation indexes", "error", err)
    return fmt.Errorf("ensure conversation indexes: %w", err)
}
indexesCancel2()
```

### Pattern 4: chat_proxy fire-points (one helper, two call sites)

**What:** Both call sites (auto/done at 578–593 and resume done at 906) need a shared helper to avoid drift. The helper reads the conversation's current `title_status`, bails if not `auto_pending`, then spawns the goroutine.

**Example:**
```go
// services/api/internal/handler/chat_proxy.go (NEW helper, called from 2 sites)

// fireAutoTitleIfPending reads the conversation, checks the gate (D-01), and
// spawns the titler goroutine. Caller passes the user message and the
// assistant message (in completion order) so the titler has the same input
// regardless of fire-point. The conversation re-read is mandatory: between
// the request reaching chat_proxy and the assistant message persist, the
// user could have manually renamed the chat.
func (h *ChatProxyHandler) fireAutoTitleIfPending(
    persistCtx func() (context.Context, context.CancelFunc),
    conversationID, businessID, userText, assistantText string,
) {
    if h.titler == nil { return } // titling disabled (TITLER_MODEL unset)

    // Use a fresh detached ctx for the read+goroutine. The original request
    // ctx may already be canceled (auto/done writes happen post-stream).
    ctx, cancel := persistCtx()
    defer cancel()

    conv, err := h.conversationRepo.GetByID(ctx, conversationID)
    if err != nil {
        slog.WarnContext(ctx, "auto-title gate: conversation lookup failed",
            "conversation_id", conversationID, "error", err)
        return
    }
    if conv.TitleStatus != domain.TitleStatusAutoPending {
        return // D-01: only fires on auto_pending; manual + auto are terminals.
    }

    // Spawn long-lived ctx (titler must outlive the request). Pattern matches
    // pkg/llm/router.go:186 `go r.logBilling(context.Background(), ...)`.
    spawnCtx, _ := persistCtx() // separate from outer ctx; do NOT share cancel
    go h.titler.GenerateAndSave(spawnCtx, businessID, conversationID, userText, assistantText)
}
```

Call site 1 — `chat_proxy.go:590` (auto/done branch), append after the existing `messageRepo.Create`:
```go
if err := h.messageRepo.Create(saveCtx, assistantMsg); err != nil {
    slog.ErrorContext(saveCtx, "failed to save assistant message", "error", err)
}
// NEW: fire titler after assistant message is persisted with Status=complete.
// The user message text is in req.Message; the assistant text is in
// assistantText.String(); both are already redacted of any HTTP transport
// concerns by being plain Go strings.
h.fireAutoTitleIfPending(persistCtx, conversationID, business.ID.String(), req.Message, assistantText.String())
```

Call site 2 — `chat_proxy.go:906` (streamResume done branch), append after `messageRepo.Update`:
```go
case "done":
    msg.Status = domain.MessageStatusComplete
    msg.Content = postText.String()
    saveCtx, cancel := persistCtx()
    if err := h.messageRepo.Update(saveCtx, &msg); err != nil {
        slog.WarnContext(saveCtx, "resume: failed to persist completed message", ...)
    }
    cancel()
    // NEW: resume completion also counts as a triggering event (D-01).
    // The user message preceding the resumed turn is the most recent user
    // message in conversation history; we look it up via messageRepo.
    h.fireAutoTitleIfPendingFromResume(persistCtx, conversationID, &msg)
    return
```

`fireAutoTitleIfPendingFromResume` is a thin variant that fetches the most recent user message via `messageRepo.ListByConversationID(ctx, conversationID, 100, 0)` and selects the latest `role=="user"` entry — needed because the resume path doesn't have `req.Message` in scope. **The planner can collapse this into a single helper if both call sites resolve the user message the same way.**

### Pattern 5: New POST /regenerate-title endpoint

**What:** New chi route registered between the existing `/conversations/{id}/move` (line 118 in router.go) and `/projects` (line 121). Handler: validate ownership, atomically transition, fire goroutine.

**Example:**
```go
// services/api/internal/handler/titler.go (NEW)

// RegenerateTitle handles POST /api/v1/conversations/{id}/regenerate-title.
//
// Atomic state machine (D-07):
//   title_status="manual"        → 409 "Нельзя регенерировать..."
//   title_status="auto_pending"  → 409 "Заголовок уже генерируется"
//   title_status="auto" or ""    → atomic transition to "auto_pending"; fire goroutine; 200
//
// Per docs/api-design.md §URL Conventions, the action verb in the path is
// the established pattern for non-CRUD operations (precedent: Phase 16
// /pending-tool-calls/{batch_id}/resolve at router.go:130).
func (h *TitlerHandler) RegenerateTitle(w http.ResponseWriter, r *http.Request) {
    userID, err := middleware.GetUserID(r.Context())
    if err != nil {
        writeJSONError(w, http.StatusUnauthorized, "unauthorized")
        return
    }

    conversationID := chi.URLParam(r, "id")
    conv, err := h.conversationRepo.GetByID(r.Context(), conversationID)
    if err != nil {
        if errors.Is(err, domain.ErrConversationNotFound) {
            writeJSONError(w, http.StatusNotFound, "conversation not found")
            return
        }
        slog.ErrorContext(r.Context(), "regenerate-title: lookup failed", "error", err)
        writeJSONError(w, http.StatusInternalServerError, "internal server error")
        return
    }
    if conv.UserID != userID.String() {
        writeJSONError(w, http.StatusForbidden, "forbidden")
        return
    }

    // D-02: manual is sovereign.
    if conv.TitleStatus == domain.TitleStatusManual {
        writeJSON(w, http.StatusConflict, map[string]string{
            "error": "title_is_manual",
            "message": "Нельзя регенерировать — вы уже переименовали чат вручную",
        })
        return
    }

    // D-03: in-flight job already running.
    if conv.TitleStatus == domain.TitleStatusAutoPending {
        writeJSON(w, http.StatusConflict, map[string]string{
            "error": "title_in_flight",
            "message": "Заголовок уже генерируется",
        })
        return
    }

    // Atomic transition auto → auto_pending. We re-use UpdateTitleIfPending's
    // sibling: a separate repo method that flips status only (no title write).
    // Need new repo method: TransitionToAutoPending(ctx, id) → matches
    // {_id, title_status: {$in: ["auto", null]}} and sets {title_status:"auto_pending"}.
    if err := h.conversationRepo.TransitionToAutoPending(r.Context(), conversationID); err != nil {
        if errors.Is(err, domain.ErrConversationNotFound) {
            // Race: someone (unlikely but possible) flipped to manual between
            // the read above and this update. Re-fetch to choose the right
            // 409 body, or just return generic 409.
            writeJSON(w, http.StatusConflict, map[string]string{
                "error": "title_state_changed",
                "message": "Заголовок изменился — обновите страницу",
            })
            return
        }
        slog.ErrorContext(r.Context(), "regenerate-title: transition failed", "error", err)
        writeJSONError(w, http.StatusInternalServerError, "internal server error")
        return
    }

    // Fire titler. Need user+assistant text — pull the last 2 messages.
    msgs, _ := h.messageRepo.ListByConversationID(r.Context(), conversationID, 100, 0)
    var userText, assistantText string
    for i := len(msgs) - 1; i >= 0 && (userText == "" || assistantText == ""); i-- {
        if assistantText == "" && msgs[i].Role == "assistant" && msgs[i].Status == domain.MessageStatusComplete {
            assistantText = msgs[i].Content
        }
        if userText == "" && msgs[i].Role == "user" {
            userText = msgs[i].Content
        }
    }

    persistCtx := func() (context.Context, context.CancelFunc) {
        return context.WithTimeout(context.Background(), 30*time.Second)
    }
    ctx, _ := persistCtx()
    go h.titler.GenerateAndSave(ctx, conv.BusinessID, conversationID, userText, assistantText)

    w.WriteHeader(http.StatusOK) // 200, no body — fire-and-forget
}
```

The corresponding new repo method `TransitionToAutoPending` follows the same pattern as `UpdateTitleIfPending` but with a different filter and update.

### Pattern 6: Frontend dropdown — conditional regenerate item

**What:** In `services/frontend/app/(app)/chat/page.tsx` between lines 128 (DropdownMenuSeparator) and 129 (MoveChatMenuItem), add a new conditional item.

**Example:**
```tsx
// services/frontend/app/(app)/chat/page.tsx (MODIFIED)
// Source: existing kebab pattern at lines 117–141.

import { Pencil, RefreshCw, Trash2, MoreHorizontal, Plus, MessageCircle } from 'lucide-react';
import { toast } from 'sonner';

interface Conversation {
  id: string;
  title: string;
  titleStatus?: 'auto_pending' | 'auto' | 'manual'; // NEW
  createdAt: string;
  projectId?: string | null;
}

// Inside ConversationItem's <DropdownMenuContent>:
<DropdownMenuItem
  onClick={(e) => {
    e.stopPropagation();
    setDraft(conv.title);
    setEditing(true);
  }}
>
  <Pencil size={14} className="mr-2" />
  Переименовать
</DropdownMenuItem>

{/* NEW — D-12: hide when manual; D-02 hard-rule. */}
{conv.titleStatus !== 'manual' && (
  <DropdownMenuItem
    onClick={(e) => {
      e.stopPropagation();
      onRegenerateTitle();
    }}
  >
    <RefreshCw size={14} className="mr-2" />
    Обновить заголовок
  </DropdownMenuItem>
)}

<DropdownMenuSeparator />
<MoveChatMenuItem conversationId={conv.id} currentProjectId={conv.projectId ?? null} />
<DropdownMenuSeparator />
<DropdownMenuItem className="text-red-600 focus:text-red-600" onClick={...}>
  <Trash2 size={14} className="mr-2" />
  Удалить
</DropdownMenuItem>
```

The `onRegenerateTitle` mutation (parent-defined, mirrors `renameConversation`):
```tsx
const { mutate: regenerateTitle } = useMutation({
  mutationFn: (id: string) =>
    api.post(`/conversations/${id}/regenerate-title`).then((r) => r.data),
  onSuccess: () => queryClient.invalidateQueries({ queryKey: ['conversations'] }),
  onError: (err: AxiosError<{ message?: string }>) => {
    const msg = err.response?.data?.message ?? 'Ошибка соединения';
    toast.error(msg); // Russian copy comes from server body.
  },
});
```

Render fallback for `"Новый диалог"` (D-09):
```tsx
const displayTitle =
  conv.title === '' || conv.titleStatus === 'auto_pending'
    ? 'Новый диалог'
    : conv.title;
// In JSX: <p className="truncate font-medium">{displayTitle}</p>
```

### Pattern 7: React Query invalidation in useChat

**What:** Single `queryClient.invalidateQueries({ queryKey: ['conversations'] })` call inside `useChat.ts`'s `handleSSEEvent` when type is `'done'`. NEVER mux titles into chat SSE.

**Example:**
```ts
// services/frontend/hooks/useChat.ts (MODIFIED — line ~265 inside handleSSEEvent)

import { useQueryClient } from '@tanstack/react-query';

// At the top of useChat:
const queryClient = useQueryClient();

// In handleSSEEvent:
const handleSSEEvent = useCallback((event: Record<string, unknown>) => {
  if (event.type === 'tool_approval_required') {
    // ... existing pause path (Phase 17) ...
    return;
  }

  // NEW (D-10): on every chat 'done', invalidate the conversations list so
  // the auto-title (already landed via UpdateTitleIfPending in the API
  // service's titler goroutine) is picked up on the next refetch tick.
  if (event.type === 'done') {
    queryClient.invalidateQueries({ queryKey: ['conversations'] });
  }

  // ... existing message-mutation path ...
}, [queryClient]);
```

### Pattern 8: Isolated ChatHeader subtree (PITFALLS §13 mitigation)

**What:** Per D-11 (USER OVERRIDE), the chat header MUST update with the sidebar. Risk: header re-render triggers full `<ChatWindow>` re-render → flicker / scroll-jump / composer focus loss. Mitigation: extract the header into its own component subscribed to a memoized React Query selector that returns ONLY the title for this `conversationId`.

**Recommended structural recipe:**
```tsx
// services/frontend/components/chat/ChatHeader.tsx (NEW)

'use client';

import { memo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { api } from '@/lib/api';

interface Conversation {
  id: string;
  title: string;
  titleStatus?: 'auto_pending' | 'auto' | 'manual';
}

interface ChatHeaderProps {
  conversationId: string;
}

// Selector picks ONLY the title + status for the relevant conversation.
// React Query's `select` runs on every cache change but the parent
// component receives a stable string reference unless the value changes —
// `memo` then prevents re-render. PITFALLS §13 mitigation.
function useConversationTitle(conversationId: string): string {
  return useQuery<Conversation[], Error, string>({
    queryKey: ['conversations'],
    queryFn: () => api.get('/conversations').then((r) => r.data),
    select: (list) => {
      const conv = list.find((c) => c.id === conversationId);
      if (!conv) return '';
      return conv.title === '' || conv.titleStatus === 'auto_pending'
        ? 'Новый диалог'
        : conv.title;
    },
  }).data ?? '';
}

function ChatHeaderImpl({ conversationId }: ChatHeaderProps) {
  const title = useConversationTitle(conversationId);
  return (
    <header className="flex items-center px-4 py-3 border-b">
      <h1 className="text-lg font-medium truncate">{title}</h1>
    </header>
  );
}

// memo prevents re-render unless the title prop changes — the message list
// and composer below are NOT inside this subtree, so they cannot lose state.
export const ChatHeader = memo(ChatHeaderImpl);
```

Then the chat page composes:
```tsx
// services/frontend/app/(app)/chat/[id]/page.tsx (sketch)
<div className="flex flex-col h-screen">
  <ChatHeader conversationId={id} />  {/* isolated subtree */}
  <MessageList ... />                  {/* NOT a child of ChatHeader */}
  <Composer ... />                     {/* NOT a child of ChatHeader */}
</div>
```

`[VERIFIED: docs/frontend-style.md]` — server components by default; this needs `'use client'` because of hooks.
`[ASSUMED]` the existing chat detail page structure already separates header from message list. The planner should verify the actual page layout in `services/frontend/app/(app)/chat/[id]/page.tsx` and confirm the structural separation.

### Anti-Patterns to Avoid

- **Unconditional `UpdateOne({_id})`:** breaks D-02/D-08 — manual rename gets clobbered. ALWAYS use `UpdateTitleIfPending` for titler writes. (PITFALLS §12.)
- **Calling `t.titler.GenerateAndSave(r.Context(), ...)` from chat_proxy:** request ctx is canceled when the SSE stream closes — the titler's LLM call gets aborted. ALWAYS use the `persistCtx()` pattern (chat_proxy.go:166–172).
- **Logging `req.Message` or `resp.Content`:** TITLE-07 hard rule. The pii.go test suite must include a "log assertion" test that asserts no message body appears in any log.
- **Synthesizing `Tier=""` then forgetting it:** the existing rate limiter (`pkg/llm/router.go:148–153`) treats empty as "free" tier, which is fine for v1.3 single-owner but worth flagging for the planner.
- **Mux'ing title updates into chat SSE:** PITFALLS §13. Frontend invalidates `['conversations']` on `done` only.
- **A single shared regex for "all PII":** combining patterns into one mega-regex makes false-positive debugging impossible. Use ONE regex per class with a named-class lookup so D-16's `regex_class` field is meaningful.
- **Hand-rolling Russian month inflection:** the fallback `"Untitled chat <date>"` uses genitive case (`26 апреля`, not `апрель`). Use a static lookup table — Go's `time.Format` doesn't support Russian months.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| LLM provider routing for cheap titler model | A second LLM client | `pkg/llm.Router` with `ChatRequest.Model` per-call override | Already supports per-call model override at `pkg/llm/router.go:168` (`pickProvider(req.Model, req.Strategy)`). Router handles fallback, billing telemetry, rate limits — duplicating any of these is bug-prone. |
| Atomic state machine for `title_status` | `Read → if-check → Write` two-step | Mongo `findOneAndUpdate` / `UpdateOne` with `$in` filter | TOCTOU is the entire point of D-08; the two-step is exactly the bug PITFALLS §12 flags. |
| PII detection with multiple regex classes | One mega-regex `(\d{16}|\d{10}|\+\d{11}|...)` | One named regex per class + a small dispatch map | Named classes give D-16's `regex_class` field; mega-regex gives unhelpful "matched" with no provenance. |
| Russian month-name formatting for "26 апреля" | `time.Format("2 January")` and string-replace | Static `[12]string` lookup of nominative-/genitive-case Russian months | Go's `time.Format` is English-only. Translation tables are 12 entries; a parser would be 50+ LOC of UTF-8 fragility. |
| Frontend toast for 409 errors | Inline `<div>` | `import { toast } from 'sonner'` (already in `useChat.ts:2`) | `[VERIFIED: services/frontend/hooks/useChat.ts:2]` — established codebase pattern. |
| `RefreshCw` icon for regenerate menu item | Custom SVG | `lucide-react` (`<RefreshCw />`) | `[VERIFIED: services/frontend/app/(app)/chat/page.tsx:8]` — `lucide-react` already imported in this exact file. |
| Mongo index creation block | `db.collection.createIndex(...)` shell scripts | `coll.Indexes().CreateMany` at API startup | `[VERIFIED: services/api/internal/repository/pending_tool_call.go:62–94]` — the EnsurePendingToolCallsIndexes pattern is the established precedent. Idempotent and lives with code. |
| 409 Conflict response shape | Custom JSON body | Mirror Phase 16's HITL handler at `services/api/internal/handler/hitl.go:191–195` | `[VERIFIED]` `writeJSON(w, http.StatusConflict, map[string]string{"error": "...", "message": "..."})` is the established 409 pattern. |
| New chi router route | Inline route in `Setup` | Add route to existing protected group at `services/api/internal/router/router.go:107–134` | `[VERIFIED: router.go:130]` — Phase 16's `r.Post("/conversations/{id}/pending-tool-calls/{batch_id}/resolve", ...)` is the action-verb precedent for D-07's path. |
| Auto-titler "is title still pending?" gate | Read-then-fire from chat_proxy | The same `persistCtx` + `GetByID` pattern as the existing chat_proxy resume gate (chat_proxy.go:182–232) | `[VERIFIED]` — read-then-decide pattern is the standard idiom in this file. |

**Key insight:** Every primitive Phase 18 needs already exists somewhere in the codebase. The phase is essentially a **composition exercise**, not a build-from-scratch. The biggest unknown is wiring an `llm.Router` into the API service for the first time — but the wiring code can be lifted almost verbatim from `services/orchestrator/cmd/main.go:740–778` (which the planner should treat as an authoritative template).

## Common Pitfalls

### Pitfall 1: API service doesn't currently import pkg/llm

**What goes wrong:** ARCHITECTURE.md §5.2 says "in-process goroutine inside API service." Naive reading: import `pkg/llm`, construct router, done. Reality: zero existing imports of `pkg/llm` in `services/api/`, no provider keys in `services/api/internal/config/config.go`, no `OPENROUTER_API_KEY` validation, no `LLM_TIER` env var.

**Why it happens:** The Router was placed in orchestrator alone in v1.0 because chat was the only LLM consumer. v1.3 changes that.

**How to avoid:** Plan must include a wave that:
1. Adds `OpenRouterAPIKey`, `OpenAIAPIKey`, `AnthropicAPIKey`, `SelfHostedEndpoints`, `TitlerModel`, `LLMModel` fields to `services/api/internal/config/config.go` (mirror orchestrator's config.go:32–36).
2. Adds a `buildProviderOpts` helper to `services/api/cmd/main.go` (lift verbatim from orchestrator main.go:740+).
3. Constructs `llm.NewRegistry()` + `llm.NewRouter(...)` in API main.go before constructing the Titler.
4. Sets `TITLER_MODEL` (default: empty → fall back to `LLM_MODEL`; if both empty → titler is disabled and trigger gate becomes a no-op).

**Warning signs:** Plan that says "import llm.Router" without addressing config plumbing — run `grep -r llm services/api/cmd/main.go` and see zero matches. The wiring step IS the work.

### Pitfall 2: persistCtx misuse — request cancellation kills titler

**What goes wrong:** Spawn `go titler.GenerateAndSave(r.Context(), ...)`. User refreshes → request ctx canceled → titler's LLM call fails with `context canceled` mid-flight → titler logs error, status stays `auto_pending`, retry on next turn. Functional but burns tokens.

**Why it happens:** `r.Context()` is the request lifetime, not the work lifetime. `chat_proxy.go:166–172` already documents this for HITL persistence.

**How to avoid:** Always wrap titler spawns in the existing `persistCtx()` helper. Pass it as a closure to `fireAutoTitleIfPending` so both call sites share the same idiom.

**Warning signs:** Code that has `go h.titler.X(r.Context(), ...)` instead of `go h.titler.X(persistCtx(), ...)`.

### Pitfall 3: Russian numeric titles falsely matched as PII

**What goes wrong:** RU passport regex `\d{10}` and INN regex `\d{10}|\d{12}` match legitimate Russian order/check titles like `Заказ 12345 от вторника`, `Чек 9876543`, `Звонок 2026-04-15`. Title gets rejected → terminal `"Untitled chat 26 апреля"` for chats that were perfectly fine.

**Why it happens:** Bare `\d{N}` regexes have no context anchor. Russian numeric content inside titles is common.

**How to avoid:** Use bounded-context anchors (see §"Russian regex patterns" below). For passport (`\b\d{10}\b`): require a word boundary AND require the title to NOT have an alphabetic prefix immediately before the digits (i.e., not `Заказ`, `Чек`, `Звонок`). For INN: require an explicit prefix marker `(?i)\bИНН[\s:]?\d{10,12}\b` so a 10-digit number alone doesn't match. For 13–19-digit CC: require Luhn check in addition to length.

**False-positive corpus (MUST PASS):**
- `Заказ 12345 от вторника`
- `Чек 9876543`
- `Звонок 2026-04-15 10:30`
- `Заявка 7654321098 на ремонт`  (10 digits but in "Заявка" context)
- `Артикул 123456789`
- `Доход за 2025`
- `Платёж 100500`

**True-positive corpus (MUST REJECT):**
- `Спросил про 4111-1111-1111-1111` (CC)
- `Помоги с +7 (495) 123-45-67`  (RU phone)
- `Связаться с user@example.com` (email)
- `IBAN GB82WEST12345698765432` (IBAN)
- `ИНН 7707083893` (with explicit prefix)
- `Паспорт 1234 567890` (with explicit prefix or specific space pattern)

**Warning signs:** Test corpus only has true-positives — no Russian numeric titles. Phase 18 is a trust-killer if first usage rejects every legitimate "Order 12345" title.

### Pitfall 4: Forgetting omitempty on `title_status` BSON tag

**What goes wrong:** Adding `bson:",omitempty"` to `Conversation.TitleStatus` would cause the empty string `""` to serialize as a missing field. Then the `$in: [..., null]` filter would match docs that have NO `title_status` field at all — a rare-but-possible state if any path ever clears the field.

**Why it happens:** Easy to add omitempty when adding fields. Phase 15 explicitly chose NOT to add it (per STATE.md "From Phase 15": "`bson:"title_status"` already wired (no `omitempty`); Phase 18 must NOT add `omitempty`").

**How to avoid:** No new BSON tags. The existing `pkg/domain/mongo_models.go:45` carries the right shape.

**Warning signs:** Any plan that touches `mongo_models.go` should be flagged — Phase 18 doesn't need to.

### Pitfall 5: Header re-render flickering composer focus

**What goes wrong:** D-11 user override: header reads from React Query cache. Naive implementation re-renders the entire `<ChatWindow>` when the title changes → `<textarea>` loses focus mid-typing.

**Why it happens:** React reconciliation: when the parent re-renders, all children re-render unless memoized.

**How to avoid:** §"Pattern 8: Isolated ChatHeader subtree" — extract `<ChatHeader>` into a separate component subscribed via React Query's `select` to a `string` (not the whole conversations array). `memo` the component. Header is a SIBLING of `<MessageList>` and `<Composer>`, not their ancestor.

**Warning signs:** Page structure has `<ChatWindow>` as the parent of header + messages + composer. Move them to siblings of a flex column.

### Pitfall 6: Cheap model returns markdown / quoted output

**What goes wrong:** Cheap models love returning `"Заголовок: \"Запланировать пост\""` or markdown-wrapped output. Naive `UpdateTitleIfPending(ctx, id, resp.Content)` writes the literal string, including quotes.

**Why it happens:** Cheap models have weak instruction-following. The system prompt says "no quotes" but quotes still appear ~10% of the time.

**How to avoid:** `sanitizeTitle` post-processor: trim whitespace, strip leading/trailing quotes (` " ' « » “ ” `), strip trailing punctuation, cap at 80 chars. Document that the cap is character count (UTF-8 runes), not byte count, since Russian chars are 2 bytes. Use `utf8.RuneCountInString` not `len()`.

**Warning signs:** Plan uses `len(title)` for the 80-char cap.

### Pitfall 7: Trigger gate fires on every assistant message — cost blowup

**What goes wrong:** PITFALLS §15 — naive trigger fires the LLM call on every user message. With D-04's "no attempt counter" policy, the only thing protecting against cost blowup is the `auto_pending` filter.

**Why it happens:** The trigger gate at chat_proxy needs to actually CHECK `title_status` before spawning. If the check is wrong (e.g., reading the conv before message persist where status is still `auto_pending` even though a previous successful titler already wrote `auto`), every turn re-fires.

**How to avoid:** `fireAutoTitleIfPending` must `GetByID` AFTER `messageRepo.Create` returns successfully and check `conv.TitleStatus == "auto_pending"`. If a prior turn's titler already succeeded, status is now `auto` and the gate correctly returns. The cost bound is "user-message volume × probability(prior titler failed)" not "user-message volume."

**Warning signs:** Plan that fires the goroutine without re-reading the conversation, or reads it from a cached pre-message-persist snapshot.

### Pitfall 8: Test suite asserts "PII matched" instead of asserting log shape

**What goes wrong:** Test for D-16 says `assert containsLogField("regex_class")`. Test passes. But a programmer adding a "matched substring" debug field doesn't trigger any failure. PII leaks into production logs.

**Why it happens:** D-16 is a "MUST NOT log X" rule, not a "MUST log Y" rule. Negative assertions are easier to break than positive.

**How to avoid:** Test should assert **the entire log line as bytes does not contain any character of the matched PII string**. E.g.: titler input is `"my SSN is 123-45-6789"` → after the titler runs, capture `slogtest.Logger`'s output → assert `!strings.Contains(output, "123-45-6789")` AND `!strings.Contains(output, "SSN")`. Repeat for every PII test case. This catches "I added a field to help debug" regressions.

**Warning signs:** Tests assert presence of fields but not absence of bodies.

## Code Examples

### Russian PII regex patterns (concrete Go regexp strings)

```go
// pkg/security/pii.go (NEW)
// Source: composed from Russian regulatory definitions + RFC 5322 + Luhn.

package security

import (
    "regexp"
    "strings"
    "unicode"
)

// Pattern strings — kept as named-class map so callers can identify which
// class fired (D-16 regex_class field) and so tests can iterate over them.

// Email — RFC 5322 simplified; covers >99% of practical inputs.
// [CITED: RFC 5322 §3.4.1 + standard practical regex]
var reEmail = regexp.MustCompile(
    `[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`,
)

// Credit card — 13–19 digits, optional separators (- or space). Luhn check
// is enforced separately by ContainsPII because pure regex over-matches
// numeric titles. The regex extracts candidate sequences; the Luhn pass
// (luhnValid) confirms.
var reCreditCard = regexp.MustCompile(
    `\b(?:\d[ \-]?){12,18}\d\b`,
)

// RU phone — three forms (E.164 +7XXXXXXXXXX, +7 (XXX) XXX-XX-XX,
// 8 XXX XXX-XX-XX). Anchored to word boundaries. Allows optional spacing
// inside the body but requires all 11 digits.
// [CITED: ITU-T E.164 + Russian numbering plan: country code +7, 10-digit subscriber]
var rePhoneRU = regexp.MustCompile(
    `(?:\+7|8)[\s\-(]*\d{3}[\s\-)]*\d{3}[\s\-]*\d{2}[\s\-]*\d{2}\b`,
)

// IBAN — country (2 letters) + 2 check digits + 11–30 alphanumeric.
// Anchored to word boundaries. Country letters MUST be uppercase per ISO 13616.
// [CITED: ISO 13616-1:2007 — IBAN structure]
var reIBAN = regexp.MustCompile(
    `\b[A-Z]{2}\d{2}[A-Z0-9]{11,30}\b`,
)

// RU passport — exactly 10 digits, but ONLY when prefixed by "паспорт"
// (case-insensitive), the explicit "серия и номер" tag, or appearing as
// "DDDD DDDDDD" (Russian passport format: 4-digit series + 6-digit number).
// Bare 10-digit numbers (Заказ 1234567890) DO NOT match.
// [ASSUMED] anchoring on prefix is the only viable approach without
// triggering the Заказ/Заявка/Артикул false-positive class.
var rePassportRU = regexp.MustCompile(
    `(?i)(?:паспорт|серия\s+и\s+номер)[\s:№]*\d{4}\s*\d{6}\b|\b\d{4}\s\d{6}\b`,
)

// INN — exactly 10 (legal entity) or 12 (individual) digits, but ONLY when
// prefixed by "ИНН" or "INN" (case-insensitive). Bare numbers don't match.
// [CITED: Federal Tax Service (RF) — INN format]
var reINN = regexp.MustCompile(
    `(?i)\b(?:ИНН|INN)[\s:№]*\d{10}(?:\d{2})?\b`,
)

// Name + bound to class for D-16's regex_class field.
type piiClass struct {
    name    string
    pattern *regexp.Regexp
    extra   func(string) bool // optional Luhn / extra validation
}

var piiClasses = []piiClass{
    {"email", reEmail, nil},
    {"phone", rePhoneRU, nil},
    {"iban", reIBAN, nil},
    {"passport", rePassportRU, nil},
    {"inn", reINN, nil},
    // CC must pass Luhn — regex alone over-matches numeric titles.
    {"cc", reCreditCard, luhnValid},
}

// ContainsPII reports whether s contains any PII pattern.
func ContainsPII(s string) bool {
    _, hit := ContainsPIIClass(s)
    return hit
}

// ContainsPIIClass reports the first matching class name (or "", false).
// D-16 uses the class name as the regex_class log field.
func ContainsPIIClass(s string) (string, bool) {
    for _, c := range piiClasses {
        loc := c.pattern.FindStringIndex(s)
        if loc == nil { continue }
        match := s[loc[0]:loc[1]]
        if c.extra != nil && !c.extra(match) { continue }
        return c.name, true
    }
    return "", false
}

// RedactPII replaces every PII match in s with the placeholder "[Скрыто]".
// D-14: applied to the user message and the assistant message BEFORE they
// reach the cheap LLM endpoint.
func RedactPII(s string) string {
    out := s
    for _, c := range piiClasses {
        // Use ReplaceAllStringFunc so we can apply the Luhn extra-check
        // before deciding to redact a CC candidate.
        out = c.pattern.ReplaceAllStringFunc(out, func(match string) string {
            if c.extra != nil && !c.extra(match) {
                return match // not a real PII match — leave intact
            }
            return "[Скрыто]"
        })
    }
    return out
}

// luhnValid implements the Luhn checksum used by major card schemes.
// [CITED: ISO/IEC 7812-1:2017 — Luhn algorithm for IIN validation]
func luhnValid(card string) bool {
    digits := make([]int, 0, 19)
    for _, r := range card {
        if unicode.IsDigit(r) {
            digits = append(digits, int(r-'0'))
        }
    }
    if len(digits) < 13 || len(digits) > 19 { return false }
    var sum int
    for i, d := range digits {
        // Double every second digit from the right.
        if (len(digits)-i)%2 == 0 {
            d *= 2
            if d > 9 { d -= 9 }
        }
        sum += d
    }
    return sum%10 == 0
}
```

### Table-driven PII tests

```go
// pkg/security/pii_test.go (NEW)
package security

import "testing"

func TestContainsPII(t *testing.T) {
    cases := []struct {
        name      string
        input     string
        wantHit   bool
        wantClass string
    }{
        // True positives.
        {"valid CC with luhn", "Спросил про 4111111111111111", true, "cc"},
        {"valid CC dashed", "Платёж 4111-1111-1111-1111", true, "cc"},
        {"RU phone +7 fmt", "Связь +7 (495) 123-45-67", true, "phone"},
        {"RU phone 8 fmt", "Звонить 8 495 123 45 67", true, "phone"},
        {"email", "Письмо на user@example.com", true, "email"},
        {"IBAN UK", "Банк GB82WEST12345698765432", true, "iban"},
        {"INN 10 with prefix", "Контрагент ИНН 7707083893", true, "inn"},
        {"INN 12 with prefix", "ИНН: 770708388912", true, "inn"},
        {"passport with prefix", "паспорт 1234 567890 РФ", true, "passport"},

        // False positives — Russian numeric titles MUST NOT match.
        {"Заказ 12345", "Заказ 12345 от вторника", false, ""},
        {"Чек 9876543", "Чек 9876543", false, ""},
        {"Звонок with date", "Звонок 2026-04-15 10:30", false, ""},
        {"Заявка 10 digits no prefix", "Заявка 7654321098", false, ""}, // 10 digits but no INN/passport prefix
        {"Артикул", "Артикул 123456789", false, ""},
        {"Доход за 2025", "Доход за 2025 квартал 1", false, ""},
        {"Платёж 100500", "Платёж 100500", false, ""},
        {"random short num", "Стол 5", false, ""},
        {"4-digit year alone", "Отчёт 2025", false, ""},
        {"non-luhn 16 digit", "Идентификатор 1234567890123456", false, ""}, // 16 digits but fails Luhn
    }
    for _, c := range cases {
        t.Run(c.name, func(t *testing.T) {
            class, hit := ContainsPIIClass(c.input)
            if hit != c.wantHit {
                t.Fatalf("ContainsPIIClass(%q) hit=%v want=%v (class=%q)", c.input, hit, c.wantHit, class)
            }
            if hit && class != c.wantClass {
                t.Fatalf("ContainsPIIClass(%q) class=%q want=%q", c.input, class, c.wantClass)
            }
        })
    }
}

func TestRedactPII(t *testing.T) {
    cases := []struct {
        name  string
        input string
        want  string
    }{
        {"phone", "Перезвонить +7 (495) 123-45-67 утром", "Перезвонить [Скрыто] утром"},
        {"email", "user@x.ru — на почту", "[Скрыто] — на почту"},
        {"valid CC", "карта 4111-1111-1111-1111", "карта [Скрыто]"},
        {"non-luhn passes through", "id 1234-5678-9012-3456", "id 1234-5678-9012-3456"}, // fails Luhn
        {"Заказ 12345 untouched", "Заказ 12345", "Заказ 12345"},
        {"mixed", "user@x.ru и +7 495 1234567", "[Скрыто] и [Скрыто]"},
    }
    for _, c := range cases {
        t.Run(c.name, func(t *testing.T) {
            got := RedactPII(c.input)
            if got != c.want {
                t.Fatalf("RedactPII(%q) = %q, want %q", c.input, got, c.want)
            }
        })
    }
}
```

### Prometheus metric — auto_title_attempts_total

```go
// services/api/internal/service/titler_metrics.go (NEW)
// Source: pattern from pkg/metrics/llm.go and pkg/metrics/tools.go.

package service

import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
)

var autoTitleAttempts = promauto.NewCounterVec(prometheus.CounterOpts{
    Name: "auto_title_attempts_total",
    Help: "Total auto-title generation attempts by status and outcome.",
}, []string{"status", "outcome"})

// recordAttempt increments the auto_title_attempts_total counter.
//
// status values:   "started" | "success" | "failure"
// outcome values:  "ok" | "llm_error" | "json_parse" | "pii_reject" |
//                  "manual_won_race" | "persist_error" | "empty_response" |
//                  "terminal_write_error"
//
// Recommended dashboard rules:
//   - rate(auto_title_attempts_total{status="failure",outcome="llm_error"}[5m])
//     > 0.1: TITLER_MODEL endpoint may be down.
//   - rate(auto_title_attempts_total{status="failure",outcome="manual_won_race"}[5m])
//     > 0.5 of started: race-condition health check; users renaming faster
//     than the cheap model — informational, not an alert.
//   - rate(auto_title_attempts_total{status="failure",outcome="pii_reject"}[1h])
//     > 0: surfaces unexpected PII in chat content; investigate dataset.
func recordAttempt(status, outcome string) {
    autoTitleAttempts.WithLabelValues(status, outcome).Inc()
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Read-then-write title without atomic guard | Mongo `UpdateOne` with `$in` filter on state field | Phase 18 introduces (existing pattern lifted from Phase 15's `UpdateProjectAssignment`) | TOCTOU window eliminated; manual rename is sovereign by construction. |
| Title generation in-stream / blocking SSE | Fire-and-forget goroutine with detached `persistCtx` | Phase 18 introduces (pattern lifted from `pkg/llm.Router.logBilling`) | Chat latency decoupled from title latency; failures degrade silently. |
| Inline regex per-feature | Reusable `pkg/security/pii.go` with named classes | Phase 18 introduces (Phase 19 will reuse) | Single source of truth for PII; auditable; testable in isolation. |
| Title arrival via SSE side channel | React Query invalidation on `done` | Phase 18 (PITFALLS §13 hard rule) | Zero new SSE event types; no flicker; works with existing cache |
| One mega-LLM-Router shared across all use cases | Per-call `ChatRequest.Model` override | Already supported in `pkg/llm` since v1.0 | Phase 18 just uses the existing knob; no router changes needed. |

**Deprecated/outdated:**
- The early ARCHITECTURE.md §5.3 sketch suggesting `Tier="background"` is fine but optional (`pkg/llm/router.go:148–153` treats unknown tiers as `"free"` — no behavioral difference today). Planner can adopt the tier label for telemetry isolation but should not block on it.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | The cheap-model titler can be implemented as an in-process goroutine in API service rather than a separate worker | §"Pattern 1: Titler service architecture" | LOW — explicitly endorsed by ARCHITECTURE.md §5.2 + STATE.md and locked by D-15. If wrong, refactor to a Redis-backed queue is a one-day change at most. |
| A2 | The user-message text passed to the titler is the full `req.Message` from the chat request (not a truncated snippet) | §"Pattern 4: chat_proxy fire-points" | LOW — pre-redacted via `RedactPII`, capped at 30 max output tokens regardless. Only consequence of being wrong is a slightly less-informative title. |
| A3 | RU passport regex requires the explicit "паспорт"/"серия" prefix or strict `DDDD DDDDDD` whitespace-separated form to avoid false positives | §"Russian PII regex patterns" | MEDIUM — if user's first chat genuinely says `Паспорт 1234 567890` without explicit prefix, the strict whitespace pattern still catches it. But a free-form passport number embedded in a longer string might slip through. Mitigation: include the broad `\b\d{4}\s\d{6}\b` form as a secondary alternative. The planner should review the false-positive corpus during plan-check. |
| A4 | INN regex requires the explicit `ИНН`/`INN` prefix | §"Russian PII regex patterns" | LOW — without the prefix, INN is a bare 10/12-digit number that's indistinguishable from `Заказ 12345`. The choice to require a prefix is correct security/UX trade. |
| A5 | The chat header is a sibling component of message list, not an ancestor | §"Pattern 8: Isolated ChatHeader subtree" | MEDIUM — if the existing chat detail page makes header the parent of message list, the memo trick still works but planner must explicitly verify the page structure. Risk if wrong: composer-focus-loss bug after Phase 18 ships. |
| A6 | `TITLER_MODEL` env var is unset in dev → graceful disable (titler doesn't fire) | §"Titler service architecture" | LOW — this is the "fail safe by design" pattern. If wrong (titler panics), dev environments break on first chat. Mitigation: explicit nil check in `fireAutoTitleIfPending` (`if h.titler == nil { return }`). |
| A7 | The frontend's `Conversation` type currently does NOT carry `titleStatus` | §"Pattern 6: Frontend dropdown — conditional regenerate item" | LOW — verified: `services/frontend/app/(app)/chat/page.tsx:32–37` `interface Conversation` has no `titleStatus`. The plan must add it AND ensure the API serializes `titleStatus` in `GET /conversations` responses (the Go struct already has the JSON tag `json:"titleStatus"` per `pkg/domain/mongo_models.go:45`). |
| A8 | The "Untitled chat <date>" Russian formatting uses genitive case (`26 апреля`, not `26 апрель`) | §"`untitledChatRussian` helper" | LOW — planner can pick either; "26 апреля" is the more natural Russian short-form ("the 26th of April"). |
| A9 | The Phase 18 trigger gate is `title_status == "auto_pending"` (D-01 verbatim), NOT `title == ""` | §"Pattern 4: chat_proxy fire-points" | LOW — D-01 is explicit. But research draft in ARCHITECTURE.md §5.1 mentions `Title == ""` as a gate; the phase has decided AGAINST that. Reading must follow CONTEXT.md, not ARCHITECTURE.md. |

**If a claim is `[ASSUMED]` rather than `[VERIFIED]` or `[CITED]` above, the planner MUST surface it for user confirmation before locking it into a plan.**

## Open Questions

1. **Should the regenerate endpoint return the new conversation document (200 with body), or just 200 empty?**
   - What we know: D-07 says "200 immediately" without a body shape; Phase 16's resolve endpoint returns SSE (different shape, not applicable).
   - What's unclear: Frontend wants `queryClient.invalidateQueries(['conversations'])` after the click anyway (mirrors the rename mutation at `chat/page.tsx:170`), so a returned body would be redundant.
   - Recommendation: Return `200` with an empty body (`w.WriteHeader(http.StatusOK)`). Frontend invalidates and refetches. Matches the Phase 15 `/move` endpoint pattern where the post-write state is reconstituted from the refetch.

2. **What happens if `chat_proxy.go`'s resume path triggers the titler but the user message preceding the resumed turn is multiple messages back in history?**
   - What we know: The resume code path (`streamResume`) doesn't have `req.Message` in scope.
   - What's unclear: Pulling "the last user message" via `messageRepo.ListByConversationID(ctx, id, 100, 0)` and walking backward is a reasonable approximation but may pick up the wrong user message if there were multiple unanswered user turns (rare but possible).
   - Recommendation: For Phase 18, accept the imprecision — the goal is a 3–6 word title, not a forensic reconstruction. If the title misses, the user can hit Regenerate. Document the limitation in a comment.

3. **Should the new compound index `{user_id: 1, business_id: 1, title_status: 1}` also include `last_message_at: -1` for sidebar ordering?**
   - What we know: Phase 15 already created a sidebar index `{user_id, business_id, project_id, pinned, last_message_at}` (per ARCHITECTURE.md §2.4 and `BackfillConversationsV15`).
   - What's unclear: Whether `title_status` deserves to be added to the existing sidebar index (modify in place) or stays as a separate single-purpose index (D-08a).
   - Recommendation: Stick with D-08a's single-purpose index. Adding to an existing index changes its selectivity tail; safer to add a new one. Mongo allows the planner to revisit if profiling shows query plans pick the wrong one.

4. **Does the cheap model `gpt-4o-mini` satisfy the `pkg/llm` Registry requirements for the API-side Router instance?**
   - What we know: `[VERIFIED: services/orchestrator/cmd/main.go:740–778]` `buildProviderOpts` registers any model passed via `cfg.LLMModel` against `RegisterModelProvider`. The same helper would register `cfg.TitlerModel` if it's passed in.
   - What's unclear: Whether ALL providers (OpenRouter, OpenAI, Anthropic, SelfHosted) support models like `gpt-4o-mini` — they do for OpenRouter and OpenAI but Anthropic only does its own. Also whether Self-Hosted endpoints can serve a "titler" model that's different from the main model.
   - Recommendation: Default `TITLER_MODEL` to whatever `LLM_MODEL` is set to. Operator who wants cheap titling sets `TITLER_MODEL=openai/gpt-4o-mini` explicitly via OpenRouter.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| MongoDB | `UpdateTitleIfPending`, new index | ✓ | 7 (per docker-compose) | — |
| Go workspace | New `pkg/security/`, edits to `services/api/` | ✓ | 1.24+ (per `go.work`) | — |
| `pkg/llm/providers` | API-side Router wiring | ✓ | already in repo | — |
| `OPENROUTER_API_KEY` (or `OPENAI_API_KEY` or `ANTHROPIC_API_KEY`) at API-service runtime | Cheap-model LLM call | ✓ in production; **NOT** currently injected to API service | (per `services/api/internal/config/config.go`) | If unset and `TITLER_MODEL` unset → titler disables gracefully (A6). |
| `lucide-react` `RefreshCw` icon | Frontend dropdown item | ✓ | 0.564.0 | — |
| `sonner` toast | Frontend 409 surfacing | ✓ | 2.0.7 | — |
| `@tanstack/react-query` `invalidateQueries` | Frontend cache invalidation | ✓ | 5.90.21 | — |

**Missing dependencies with no fallback:**
- None.

**Missing dependencies with fallback:**
- LLM provider keys not injected to API service today — if not added in Phase 18 plan, titler is permanently disabled. Plan MUST add `OPENROUTER_API_KEY` (etc.) to the API service's `.env.example` and `services/api/internal/config/config.go`. Existing keys in production environment can be reused (single-source-of-truth via shared `.env`).

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | **Go**: `testing` (stdlib) + `testify/v1.11.1` for assertions, mocks, integration. **Frontend**: Vitest 4.0.18 + Testing Library + jsdom. |
| Config file | `services/api/go.mod`, `pkg/go.mod`, `services/frontend/vitest.config.ts`, `services/frontend/vitest.setup.ts` |
| Quick run command (Go pkg/security) | `cd /Users/f1xgun/onevoice/.worktrees/milestone-1.3/pkg && GOWORK=off go test -race ./security/...` |
| Quick run command (Go services/api/service titler) | `cd /Users/f1xgun/onevoice/.worktrees/milestone-1.3/services/api && GOWORK=off go test -race ./internal/service/...` |
| Quick run command (frontend useChat) | `cd /Users/f1xgun/onevoice/.worktrees/milestone-1.3/services/frontend && pnpm test -- hooks/useChat` |
| Full suite command | `make test-all` from repo root |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| TITLE-01 | Sidebar renders `"Новый диалог"` placeholder when title=="" or auto_pending | unit (frontend) | `cd services/frontend && pnpm test -- chat/page` | ❌ Wave 0 |
| TITLE-02 | After first complete assistant reply, titler goroutine fires with `TITLER_MODEL` | unit (backend, mocked Router) | `cd services/api && GOWORK=off go test -race ./internal/service/... -run TestTitler_GenerateAndSave_HappyPath` | ❌ Wave 0 |
| TITLE-03 | PUT /conversations/{id} sets title_status="manual" unconditionally | unit (handler) | `cd services/api && GOWORK=off go test -race ./internal/handler/... -run TestUpdateConversation_FlipsTitleStatusManual` | ❌ Wave 0 |
| TITLE-04 | UpdateTitleIfPending no-ops when status="manual"; succeeds when "auto_pending" | unit (repository) + integration (Mongo) | `cd services/api && GOWORK=off go test -race ./internal/repository/... -run TestConversationRepo_UpdateTitleIfPending` | ❌ Wave 0 |
| TITLE-05 | Title job failure leaves status unchanged at auto_pending | unit (titler with mock Router error) | `cd services/api && GOWORK=off go test -race ./internal/service/... -run TestTitler_GenerateAndSave_LLMError_StatusUnchanged` | ❌ Wave 0 |
| TITLE-06 | useChat invalidates ['conversations'] on SSE 'done' (no flicker) | unit (frontend hook) | `cd services/frontend && pnpm test -- hooks/useChat -t "invalidates conversations on done"` | ❌ Wave 0 |
| TITLE-07 | slog output never contains prompt/response body bytes | unit (titler with `slogtest`) | `cd services/api && GOWORK=off go test -race ./internal/service/... -run TestTitler_LogShape_NoPIIInLog` | ❌ Wave 0 |
| TITLE-08 | PII match → terminal "Untitled chat <date>" with status="auto" | unit (titler) + table-driven (pkg/security/pii) | `cd services/api && GOWORK=off go test -race ./internal/service/... -run TestTitler_PIIRejected_TerminalFallback` AND `cd pkg && GOWORK=off go test -race ./security/... -run TestContainsPII` | ❌ Wave 0 |
| TITLE-09 | POST /regenerate-title transitions auto→auto_pending; 409 on manual / in-flight | unit (handler) + integration | `cd services/api && GOWORK=off go test -race ./internal/handler/... -run TestRegenerateTitle` | ❌ Wave 0 |

### Sampling Rate

- **Per task commit:** `cd <module> && GOWORK=off go test -race ./...` for whichever module the task modifies (`pkg/security`, `services/api`, `services/frontend`).
- **Per wave merge:** `make lint-all && make test-all` from repo root.
- **Phase gate:** Full suite green + manual UAT covering: (a) new chat → title appears in sidebar within ~10s, (b) rename mid-typing — auto-titler doesn't clobber, (c) regenerate menu item refreshes title, (d) 409 toast on regenerate-while-manual, (e) PII chat (`Спросил про 4111-1111-1111-1111`) → "Untitled chat <date>" fallback, (f) Loki grep for `prompt body` returns zero hits over the test window.

### Wave 0 Gaps

- [ ] `pkg/security/pii.go` — does not exist; phase creates it.
- [ ] `pkg/security/pii_test.go` — does not exist; phase creates it.
- [ ] `services/api/internal/service/titler.go` — does not exist; phase creates it.
- [ ] `services/api/internal/service/titler_test.go` — does not exist; phase creates it (with mocked `llm.Router` and `domain.ConversationRepository`).
- [ ] `services/api/internal/handler/titler.go` — does not exist; phase creates it.
- [ ] `services/api/internal/handler/titler_test.go` — does not exist; phase creates it.
- [ ] `services/frontend/components/chat/ChatHeader.tsx` — may exist as part of existing chat page; phase isolates it as a memoized subtree (verify during planning).
- [ ] No frontend test file currently covers `useChat`'s `done` invalidation — phase adds it.
- [ ] Mongo integration test for `UpdateTitleIfPending` — fits the existing pattern in `services/api/internal/repository/*_test.go` (verify file naming conventions during plan-check).

**Validation note:** All boundary conditions specified in the research focus area #12 are covered by the table above. Specifically:
- Regex false-positive corpus → `TestContainsPII` table-driven cases (see code in §"Table-driven PII tests")
- Manual-rename-mid-job race → `TestConversationRepo_UpdateTitleIfPending` simulates a mid-flight `Update(... title_status:"manual")` and asserts the conditional update returns `MatchedCount=0`
- Regenerate-on-manual 409 → `TestRegenerateTitle_ManualConflict`
- Regenerate-on-in-flight 409 → `TestRegenerateTitle_InFlightConflict`
- LLM error → no status change → `TestTitler_GenerateAndSave_LLMError_StatusUnchanged`
- JSON parse failure → no status change → handled inside `sanitizeTitle` which yields empty + early-return without persist
- PII match → terminal `Untitled chat <date>` → `TestTitler_PIIRejected_TerminalFallback`
- title === "" rendering → `TestConversationItem_RendersPlaceholderWhenTitleEmpty`
- title_status === "auto_pending" rendering → `TestConversationItem_RendersPlaceholderWhenAutoPending`
- Header live-update without flicker → `TestChatHeader_MemoSkipsRerenderWhenTitleUnchanged` (assert React Profiler render count)
- Metric increments per outcome → `TestTitler_RecordsMetric_PerOutcome` using a Prometheus testutil to assert counter values

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | yes | Existing JWT middleware (`services/api/internal/middleware/auth.go`) — regenerate-title endpoint is in the protected route group. |
| V3 Session Management | partial | `Conversation.UserID` ownership check in `RegenerateTitle` handler; reuses existing pattern from `UpdateConversation` (chat_proxy.go:321). |
| V4 Access Control | yes | Conversation ownership: `if conversation.UserID != userID.String() → 403`. Standard pattern. |
| V5 Input Validation | yes | `pkg/security/pii` — pre-redact (D-14) AND post-hoc validate (D-13) on titler I/O. |
| V6 Cryptography | no | No new crypto introduced. Existing AES-256-GCM (`pkg/crypto`) untouched. |
| V7 Error Handling | yes | Structured error responses (writeJSON + Russian-message map for 409). No internal stack traces leak. |
| V8 Data Protection | yes | TITLE-07: log metadata only — never prompt body, never response body, never matched PII substring. Auto-title goes onto the `docs/security.md` logging-exemption list. |
| V12 Files and Resources | no | No file upload/download surface in Phase 18. |

### Known Threat Patterns for Go + LLM Titling

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| PII in titler input → leaked into provider's prompt | Information Disclosure | Pre-redact via `RedactPII` BEFORE the LLM call (D-14). |
| Generated title contains PII echo from chat content | Information Disclosure | Post-hoc `ContainsPII` gate; PII match → terminal "Untitled chat <date>" (D-05). |
| Manual rename clobbered by stale titler write | Tampering / Trust violation | Atomic `UpdateOne` with `title_status ∈ {auto_pending, null}` filter (D-08). |
| Concurrent regenerate-title clicks → double LLM cost | Repudiation / Cost abuse | 409 on `auto_pending` AND atomic `TransitionToAutoPending` (D-03 + D-07). |
| Regenerate-on-foreign-conversation (IDOR) | Elevation of Privilege | `if conv.UserID != userID → 403` ownership check in handler. |
| Logging the user message body for "debugging" | Information Disclosure | TITLE-07 hard rule; test `TestTitler_LogShape_NoPIIInLog` asserts no message body appears in any log line. |
| Cheap-model endpoint compromise / rate-limit abuse | Denial of Service | Existing `pkg/llm.RateLimiter` runs against the API-side Router instance same as orchestrator; titler obeys it via `req.UserID = uuid.Nil` (system tier). |
| Russian numeric titles wrongly redacted (UX bug, not security) | Availability (UX) | Bounded-context regexes for passport/INN (require explicit prefix); test corpus includes `Заказ 12345` etc. |
| Tier='free' default consumes user's quota for auto-title | DoS via own quota | Set `req.Tier = "background"` and `req.UserID = uuid.Nil` so titler doesn't draw from the user's request budget. |

## Sources

### Primary (HIGH confidence)

- **`pkg/llm/router.go:156–190`** — `Router.Chat()` method confirms per-call `ChatRequest.Model` override is supported. `pickProvider(req.Model, req.Strategy)` at line 168.
- **`pkg/llm/types.go:43–57`** — `ChatRequest` struct fields including `Model`, `Tier`, `MaxTokens`, `Temperature`, `UserID`.
- **`pkg/domain/mongo_models.go:1–50`** — `Conversation.TitleStatus`, `TitleStatusAutoPending|Auto|Manual` constants. Phase 15-shipped.
- **`services/api/internal/repository/conversation.go:78–133`** — Existing `Update` and `UpdateProjectAssignment` methods are the exact pattern templates.
- **`services/api/internal/repository/pending_tool_call.go:62–94`** — `EnsurePendingToolCallsIndexes` is the exact precedent for the new `EnsureConversationIndexes` helper.
- **`services/api/cmd/main.go:108–129`** — Index creation block placement at startup.
- **`services/api/internal/handler/chat_proxy.go:166–172`** — `persistCtx` helper for detached goroutine context (wraps `context.WithTimeout(context.Background(), 5*time.Second)`).
- **`services/api/internal/handler/chat_proxy.go:578–593`** — Auto/done assistant message persist (Trigger fire-point #1).
- **`services/api/internal/handler/chat_proxy.go:903–911`** — `streamResume` `done` branch (Trigger fire-point #2).
- **`services/api/internal/handler/conversation.go:292–334`** — Existing `UpdateConversation` PUT handler — D-06's modification site.
- **`services/api/internal/handler/hitl.go:191–195`** — Phase 16's 409 response shape — exact mirror for D-02 / D-03.
- **`services/api/internal/router/router.go:107–134`** — Protected route group + Phase 16's action-verb endpoint precedent.
- **`services/orchestrator/cmd/main.go:54–59, 740–778`** — `llm.Registry` + `llm.NewRouter` + `buildProviderOpts` template for API-side wiring.
- **`services/frontend/app/(app)/chat/page.tsx:117–141`** — Kebab dropdown — exact insertion point for D-12 menu item.
- **`services/frontend/hooks/useChat.ts:239–265, 67–69`** — `handleSSEEvent` + 'done' branch — exact insertion point for D-10 invalidation.
- **`services/frontend/hooks/useChat.ts:2`** — `import { toast } from 'sonner'` — established codebase pattern for D-03 toast.
- **`pkg/metrics/llm.go:1–28`** — Prometheus counter pattern.
- **`docs/api-design.md:7–46`** — REST conventions (200/409 status codes, action verbs).
- **`docs/security.md:9–45`** — PII / logging exemption / rate-limit context.
- **`.planning/research/ARCHITECTURE.md §5`** — Auto-titler architecture (in-process goroutine, atomic update).
- **`.planning/research/PITFALLS.md §12–§16`** — Title race, flicker, JSON failure, cost blowup, PII leak.
- **`.planning/research/STACK.md §c`** — Decision: reuse `pkg/llm.Router`, no new SDK.

### Secondary (MEDIUM confidence)

- **MongoDB Manual: `$in` operator** https://www.mongodb.com/docs/manual/reference/operator/query/in/ — confirmed `$in` accepts BSON null. WebSearch verified.
- **MongoDB Manual: text-search Russian language analyzer** https://www.mongodb.com/docs/manual/reference/text-search-languages/ — confirmed Russian stemmer support (relevant only for Phase 19 but mentioned because index creation patterns overlap).
- **ITU-T E.164 + Russian Federal numbering plan** — phone regex shape verified against `+7` country code and 10-digit subscriber.
- **ISO 13616-1 — IBAN structure** — IBAN regex shape verified.
- **ISO/IEC 7812-1 — Luhn algorithm** — Luhn implementation verified.
- **Federal Tax Service (RF) — INN format** — INN 10/12-digit shape verified.

### Tertiary (LOW confidence)

- `[ASSUMED]` Russian passport regex requires explicit prefix to avoid false positives — security/UX trade-off; flagged in Assumptions Log A3.
- `[ASSUMED]` Existing chat detail page in `services/frontend/app/(app)/chat/[id]/page.tsx` already separates `<header>` from message list as siblings — flagged in A5; planner verifies during plan-check.
- `[ASSUMED]` Setting `Tier="background"` provides telemetry isolation — current code treats unknown tiers as `"free"` (`pkg/llm/router.go:148–153`); behavioral parity is fine for v1.3.

## Metadata

**Confidence breakdown:**

- **Standard stack:** HIGH — every library is already in `go.work` / `package.json`. No new dependencies.
- **Architecture:** HIGH — composition of established patterns; only meaningful unknown is API-side Router wiring (template lifted from orchestrator).
- **Pitfalls:** HIGH — PITFALLS §12–§16 directly map to D-01…D-16; the false-positive corpus is enumerated.
- **PII regexes:** MEDIUM — Russian-context regexes are tested against a corpus; passport/INN strict prefix is an assumption (A3, A4) the planner must surface.
- **Frontend isolation pattern:** MEDIUM — page structure assumption (A5) requires planner verification.

**Research date:** 2026-04-26
**Valid until:** 2026-05-26 (30 days; codebase is stable and Phase 18 surface area is small; nothing here references fast-moving external APIs).
