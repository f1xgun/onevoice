# Phase 18: Auto-Title - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-04-26
**Phase:** 18-Auto-Title
**Areas discussed:** Trigger & regenerate flow, Sidebar pending UX, Failure fallback, PII sanitization scope

---

## Trigger & Regenerate Flow

### Q1: Trigger gate — what conversation state must hold before the auto-title job fires?

| Option | Description | Selected |
|--------|-------------|----------|
| First COMPLETE assistant message | Fire after `Status = complete` persists; HITL pauses don't trigger. Cheapest, no double-fire. | ✓ |
| After first user+assistant pair (any state) | Fire on `messageCount >= 2` regardless of HITL pause; risks titling stub messages. | |
| After N complete turns (e.g., 2) | Wait for two full turns; better titles, longer placeholder window. | |

**User's choice:** First COMPLETE assistant message (Recommended).
**Notes:** Drives D-01. The trigger condition is later widened (per Q5 below) to fire on ANY complete assistant turn while `auto_pending`, to enable self-healing retries.

### Q2: Regen vs manual rename — what does "Regenerate title" do when `title_status = manual`?

| Option | Description | Selected |
|--------|-------------|----------|
| Refuse — manual is sovereign | 409 with Russian error toast; frontend hides menu item. Aligns with PITFALLS §12. | ✓ |
| Allow with explicit confirm dialog | Power-user friendly, more code. | |
| Always allow silently | Simplest, but a misclick blows away a deliberate name. Violates trust-critical promise. | |

**User's choice:** Refuse — manual is sovereign (Recommended).
**Notes:** Drives D-02. Frontend hides the menu item entirely when `title_status === 'manual'`.

### Q3: Concurrent regenerate (in-flight job) — what happens when user double-clicks Regenerate?

| Option | Description | Selected |
|--------|-------------|----------|
| Idempotent — second call is a no-op (Recommended) | Server returns 200 with current state, no second goroutine. | |
| 409 conflict, frontend toasts "уже генерируется" | Visible state feedback; user knows the job is in progress. | ✓ |
| Force re-run — cancel old, start new | Adds context-cancellation plumbing for almost no value. | |

**User's choice:** 409 conflict (override of recommendation).
**Notes:** Drives D-03. User overrode the idempotent recommendation in favor of explicit visible feedback. Pattern: user prefers visible state over silent success when the action is user-initiated. Captured as user feedback memory candidate.

### Q4: Manual flip — when does PUT /conversations/{id} set `title_status = "manual"`?

| Option | Description | Selected |
|--------|-------------|----------|
| Always on PUT, regardless of value | Predictable, simple, frontend zero-change. | ✓ |
| Only when title actually changes | Saves a no-op write; adds read-before-write. | |
| Add explicit flag to PUT body | Most explicit, requires frontend change. Useful only with multiple PUT callers. | |

**User's choice:** Always on PUT (Recommended).
**Notes:** Drives D-06.

---

## Sidebar Pending UX

### Q5: Pending row — what does the sidebar show during `auto_pending` (3–8s gap)?

| Option | Description | Selected |
|--------|-------------|----------|
| "Новый диалог" placeholder, swap on update | Matches TITLE-01 verbatim; current frontend already passes this string. | ✓ |
| Animated shimmer / skeleton row | Visually communicates "working…" but adds noise on every brand-new chat. | |
| "Новый диалог" + subtle dot/icon | Halfway; adds an icon component. | |

**User's choice:** "Новый диалог" placeholder (Recommended).
**Notes:** Drives D-09.

### Q6: Title arrival — how does the sidebar pick up the new title?

| Option | Description | Selected |
|--------|-------------|----------|
| React Query invalidate on chat-stream `done` event | One-line change in useChat; no new endpoint, no polling, no flicker. | ✓ |
| Periodic poll on chat list page | Simple but wastes requests when idle. | |
| Dedicated SSE side channel `/conversations/stream` | Architecturally clean but premature; nothing else uses it yet. | |
| Refetch on window focus / navigation | Misses the 3–8s window entirely. | |

**User's choice:** React Query invalidate on `done` (Recommended).
**Notes:** Drives D-10.

### Q7: Header sync — what does the chat header show, and how to handle flicker?

| Option | Description | Selected |
|--------|-------------|----------|
| Header reads from local state, frozen at chat-open (Recommended) | Aligns with PITFALLS §13 prevention. User sees new title only on next navigation. | |
| Header updates live with sidebar | Simpler code (one source of truth) but PITFALLS §13 warns of scroll-jump / focus-loss / composer flicker. | ✓ |
| Header shows date placeholder "Диалог 26 апр 14:30" | More distinctive but inconsistent with sidebar. | |

**User's choice:** Header updates live with sidebar (override of recommendation).
**Notes:** Drives D-11. User explicitly accepted PITFALLS §13's flicker risk in exchange for simpler/faster-feedback UX. Planner MUST structure the header as an isolated React subtree (memoized, `title`-only selector) to mitigate. Documented as non-negotiable mitigation in CONTEXT.md `<specifics>`.

### Q8: Regen UI — where does the user trigger "Regenerate title"?

| Option | Description | Selected |
|--------|-------------|----------|
| Sidebar context menu only | Single affordance, easy to find; hidden when `manual`. | ✓ |
| Both sidebar context menu AND chat header dropdown | More UI surface; more components to maintain (Phase 19 redesigns sidebar). | |
| Chat header only (no context menu) | Less discoverable from list view; inconsistent with rename. | |

**User's choice:** Sidebar context menu only (Recommended).
**Notes:** Drives D-12.

---

## Failure Fallback

### Q9: Failure end — what's the final state when title job fails after 1 retry?

| Option | Description | Selected |
|--------|-------------|----------|
| Stay in `auto_pending`, retry on next user turn | Self-healing for transient failures; needs trigger gate widened. | ✓ |
| Set terminal `Untitled chat 2026-04-26` | Predictable, no retry loops; permanent fallback name for transient failures. | |
| Hybrid: retry once on next turn, then terminal date stamp | Bounded retries + deterministic state; adds counter field. | |

**User's choice:** Stay in `auto_pending` (Recommended).
**Notes:** Drives D-04.

### Q10: Trigger gate — how does retry-on-next-turn gate fire?

| Option | Description | Selected |
|--------|-------------|----------|
| Fire when `title_status = auto_pending` AND last assistant msg is complete | Drops "first message only"; atomic update prevents double-fire. | ✓ |
| Fire on first turn only, no retry | Predictable cost ceiling but bad UX for transient failures. | |
| Fire on first turn + on Regenerate explicit click | Cost-capped, requires user action to recover. | |

**User's choice:** Fire on every complete turn while auto_pending (Recommended).
**Notes:** Drives D-01 widening + D-04.

### Q11: PII rejection — different failure mode, how to handle?

| Option | Description | Selected |
|--------|-------------|----------|
| PII rejection is terminal, set `Untitled chat <date>` | Distinguishes failure types; chat content reliably keeps producing PII echoes. | ✓ |
| PII rejection retries like everything else | Single failure path, simpler code. Cost burn for nothing. | |
| PII rejection terminal at attempt counter ≥ 2 | Adds counter field for one fluky retry. | |

**User's choice:** PII rejection is terminal (Recommended).
**Notes:** Drives D-05.

### Q12: Cost cap — hard cap on attempts per conversation lifecycle?

| Option | Description | Selected |
|--------|-------------|----------|
| No counter — trust trigger gate | Bounded by user-message volume; simpler code; metric for visibility. | ✓ |
| Hard cap at N=3 attempts, then terminal | Belt-and-suspenders against cheap-model-down. | |
| Cap at N=3 with exponential backoff | Most defensive, most code. Probably overkill for single-owner. | |

**User's choice:** No counter (Recommended).
**Notes:** Drives D-04. Captured for the deferred-ideas list — revisit if multi-tenant or higher volume lands.

---

## PII Sanitization Scope

### Q13: Regex set — which patterns trigger PII rejection on the generated title?

| Option | Description | Selected |
|--------|-------------|----------|
| CC + intl/RU phone + email | Matches the requirement minimum; narrow regex set, low false-positive risk. | |
| Above + IBAN + RU passport/INN (broader) | Wider PII catch; higher false-positive risk on numeric titles. | ✓ |
| Above + addresses + names (heuristic) | Heuristics will misfire constantly on Russian titles. | |

**User's choice:** Broader set (override of recommendation).
**Notes:** Drives D-13. User accepted the false-positive risk for broader coverage. Planner MUST include legitimate Russian numeric titles in the test corpus to prove no false-positives.

### Q14: Defense-in-depth — pre-redact prompt before sending to cheap model?

| Option | Description | Selected |
|--------|-------------|----------|
| Yes — redact same regex set in prompt | Belt-and-suspenders; cheap model never sees raw PII; aligns with PITFALLS §16. | ✓ |
| No — rely solely on output regex | Fewer code paths; cheap-model provider may log prompt server-side. | |
| Yes, but only when regex is heuristic-positive | Trivial perf gain, more branching. | |

**User's choice:** Yes — pre-redact (Recommended).
**Notes:** Drives D-14.

### Q15: Helper home — where does the PII regex/redaction helper live?

| Option | Description | Selected |
|--------|-------------|----------|
| `pkg/security/pii.go` | Reusable, aligns with `docs/security.md`; Phase 19 search may also want it. | ✓ |
| `services/api/internal/service/titler.go` private helpers | Cohesive but less reuse-ready; Phase 19 would have to lift it out. | |
| `pkg/security/pii.go` AND a thin wrapper in titler | Most flexible, most code. | |

**User's choice:** `pkg/security/pii.go` (Recommended).
**Notes:** Drives D-15.

### Q16: Failure log shape — what does the structured log line carry?

| Option | Description | Selected |
|--------|-------------|----------|
| Field `rejected_by: "pii_regex"` + `regex_class` | Lets ops tune regex weights without seeing PII; aligns with TITLE-07. | ✓ |
| Just `error: "pii_rejected"` + nothing else | Minimal; harder to debug regex misfires. | |
| Add `regex_class` AND a hashed prefix of matched content | Adds key-management; overkill for single-owner. | |

**User's choice:** Field `rejected_by` + `regex_class` (Recommended).
**Notes:** Drives D-16.

---

## Claude's Discretion

The following implementation details were left to the planner with guidance from research:

- TITLER_MODEL provider/fallback chain (research: fallback to LLM_MODEL if unset).
- System prompt wording for the cheap model (research draft accepted as starting point).
- Max output tokens (research: 20–30) and temperature (research: 0.3).
- Title length cap on the post-LLM trim/sanitize step (research: 60–80 chars).
- Russian date-stamp formatting for the `Untitled chat <date>` fallback.
- Backoff between failed attempts within a single chat session (none — trigger gate suffices).
- Prometheus metric names (research suggestion accepted).
- Whether the regenerate endpoint persists a system-note (likely NO — metadata-only action).
- Frontend toast component for 409 messages (reuse existing).

---

## Deferred Ideas

- **Hard cost cap with attempt counter** — explicitly rejected for v1.3. Revisit if multi-tenant or higher chat volume lands. Note for v1.4 backlog.
- **HMAC-hashed PII-rejection log fields** — rejected for single-owner deployment. Revisit if/when a SOC pipeline lands.
- **Regenerate-title system note in chat history** — discussed, deferred. Add only if a future requirement explicitly needs the LLM to see the regen.
- **Dedicated `/conversations/stream` SSE side channel** — rejected for Phase 18. Phase 19's master/detail sidebar may introduce this if real-time conversation updates beyond title arrive.
- **Title regen in chat header dropdown** — rejected for Phase 18 (sidebar context menu only). Phase 19's sidebar redesign may revisit affordance placement.
