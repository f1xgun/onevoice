# Phase 19: Modular Decomposition — Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in `19-CONTEXT.md` — this log preserves the alternatives considered.

**Date:** 2026-05-09
**Phase:** 19-modular-decomposition
**Areas discussed:** Package layout & naming, Decomposition API shapes, Sequencing/atomicity/merge, Frontend split

---

## Package layout & naming

### Q1: New wiring package name

| Option | Description | Selected |
|--------|-------------|----------|
| `wire/` | Matches Go community convention; SPEC.md proposed it; cmd/main.go calls wire.BootstrapDatabases() etc. | ✓ |
| `bootstrap/` | More explicit; describes lifecycle; no association with Wire DI library | |
| `app/` | Common in Clean Architecture / DDD; App.New() returns assembled app | |

**User's choice:** `wire/`

### Q2: pkg/agentbase/ location

| Option | Description | Selected |
|--------|-------------|----------|
| `pkg/agentbase/` (new top-level) | SPEC.md proposal; sibling to pkg/a2a, pkg/llm | ✓ |
| `pkg/a2a/agentbase/` | Sub-package of a2a; risk that a2a does too much | |
| `pkg/agent/` | Shorter; risk of confusion with services/agent-* dirs | |

**User's choice:** `pkg/agentbase/` (new top-level)

### Q3: chat_proxy.go decomposition packaging

| Option | Description | Selected |
|--------|-------------|----------|
| Sub-package `handler/chatproxy/` | Best isolation; entry handler stays as thin facade | ✓ |
| Flat files in `handler/` | Lower friction; tests stay in same package | |
| Hybrid: `handler/` entry + `handler/chat/` collaborators | Splits without burying the HTTP entry | |

**User's choice:** Sub-package `handler/chatproxy/`

### Q4: oauth.go split (handling Telegram paste-flow)

| Option | Description | Selected |
|--------|-------------|----------|
| All under `handler/oauth/` (vk/yandex/google/telegram + base) | SPEC.md pragmatic proposal | |
| Split: `handler/oauth/` (OAuth) + `handler/connect/` (paste-flow) | Stricter naming; splits concerns | ✓ |
| `handler/integration_connect/` | Bigger rename; semantic clarity | |

**User's choice:** Split `handler/oauth/` + `handler/connect/`

---

## Decomposition API shapes

### Q5: pkg/agentbase/ API shape

| Option | Description | Selected |
|--------|-------------|----------|
| Concrete struct + composition | Less ceremony; easier to extract | |
| Interface-first | Flexible; easier to mock; risk of speculative methods | ✓ |
| Hybrid: helpers + small interfaces only where needed | Per-helper choice driven by current duplication | |

**User's choice:** Interface-first

### Q6: HITL pause/resume + tool persistence boundaries

| Option | Description | Selected |
|--------|-------------|----------|
| OrchestrationProxy owns pause/resume; MessagePersister owns tool persistence | Clean separation: transport vs storage | |
| Dedicated HITLCoordinator alongside the four collaborators | 5th collaborator; narrower responsibility per file | ✓ |
| Keep pause/resume in entry handler; collaborators handle steady-state | Risk: entry handler stays large | |

**User's choice:** Dedicated HITLCoordinator (5 collaborators total)

### Q7: BusinessBrowser (Yandex pool) decomposition

| Option | Description | Selected |
|--------|-------------|----------|
| Tool-grouped files in scrapers/ and tools/ (SPEC.md) | 4 files; BusinessBrowser as thin dispatcher | |
| Per-tool files (one file per RPA action) | 7 files; easier to add per-tool tests | ✓ |
| Flat under yandex/ with file prefixes | Avoids new sub-packages | |

**User's choice:** Per-tool files

### Q8: Yandex pre-split test scope

| Option | Description | Selected |
|--------|-------------|----------|
| Add scraper-only tests (extractText, scrapeReviewCards, formatHoursForYandex) | ~6 new tests | |
| Add tests for every BusinessBrowser method touched in 19-08 | Highest safety; ~10–12 new tests | ✓ |
| Trust existing coverage + add post-split smokes | Cheapest, but late detection | |

**User's choice:** Tests for every BusinessBrowser method touched, before split

### Q9: PlatformSyncer structure

| Option | Description | Selected |
|--------|-------------|----------|
| Strategy interface + per-platform implementations | Clean polymorphism; superset interface | |
| Capability interfaces (segregated) | Each platform implements only what it supports | ✓ |
| Per-platform struct + dispatcher map | Pragmatic; matches current code | |

**User's choice:** Capability interfaces (TitleSyncer, DescriptionSyncer, PhotoSyncer, ScheduleSyncer)

### Q10: OrchestratorClient interface location

| Option | Description | Selected |
|--------|-------------|----------|
| `services/api/internal/orchestratorclient/` | Localized | |
| `pkg/orchestratorclient/` (shared) | Symmetric with pkg/tokenclient/ | ✓ |
| `services/api/internal/service/orchestrator.go` | Lightweight, no new package | |

**User's choice:** `pkg/orchestratorclient/` (shared)

### Q11: 5 chat_proxy collaborators communication

| Option | Description | Selected |
|--------|-------------|----------|
| Direct method calls + plain return values | Clear, testable, no shared state | ✓ |
| Event channels (publish/subscribe) | More decoupled; concurrency complexity | |
| Pipeline via callbacks | Functional style; harder to short-circuit | |

**User's choice:** Direct method calls + plain return values

---

## Sequencing, atomicity & merge coordination

### Q12: Commit granularity within a plan

| Option | Description | Selected |
|--------|-------------|----------|
| Strict one-commit-per-plan | Easier bisect/revert; harder to review | |
| Sub-commits allowed; PR groups under the plan | Better review experience; less atomic revert | ✓ |
| Strict per-plan + 'tests-first' carve-out | ~14 commits; explicit risk #3/#5 mitigation | |

**User's choice:** Sub-commits allowed

### Q13: Freeze window on chat_proxy.go and oauth.go

| Option | Description | Selected |
|--------|-------------|----------|
| No freeze — worktree isolation handles it | Author rebases at merge time | ✓ |
| Soft freeze: 1–2 days while 19-03/04 land | SPEC risk #2 mitigation explicit | |
| Hard freeze: CODEOWNERS gate | Probably overkill | |

**User's choice:** No freeze — worktree isolation handles it

### Q14: Merge ordering for the 12 plans

| Option | Description | Selected |
|--------|-------------|----------|
| SPEC.md proposed order (19-01 → 19-12) | Backend first, frontend last; natural deps | ✓ |
| Hot files first (19-03 / 19-04) | Aggressive risk #2 mitigation | |
| Frontend first | Smaller diffs first | |

**User's choice:** SPEC.md order — refined by Q19 (frontend in parallel)

### Q15: Test policy for tests reaching into private types

| Option | Description | Selected |
|--------|-------------|----------|
| Import-path-only updates allowed | SPEC criterion 3 standard | ✓ |
| Prefer rewriting to public API | Higher quality, more cost | |
| Allow either, plan-author's call | Velocity-optimized | |

**User's choice:** Import-path-only updates; assertions identical

### Q16: AGENTS.md update timing

| Option | Description | Selected |
|--------|-------------|----------|
| Inline with each plan's commit | Reviewers see structure + docs together | |
| Single end-of-phase commit (plan 19-13) | Cleaner doc-only PR; docs lag | ✓ |
| Inline + closing pass | Safety net | |

**User's choice:** Single end-of-phase commit — adds plan 19-13

### Q17: Verification checkpoint between plans

| Option | Description | Selected |
|--------|-------------|----------|
| `make lint-all && make test-all` per commit | Hard CI gate; SPEC criterion 2 | ✓ |
| Per-commit + smoke after each high-risk plan | Catches flow regressions early | |
| Per-commit + single end-of-phase smoke | Fastest cadence | |

**User's choice:** Per-commit lint+test; single smoke at end

---

## Frontend split

### Q18: useChat ⇄ usePendingApprovalFlow relationship

| Option | Description | Selected |
|--------|-------------|----------|
| Sibling hooks consumed in parallel by ChatPage | Loosest coupling | ✓ |
| useChat composes usePendingApprovalFlow internally | Refactor invisible to consumers | |
| useChat returns slice; usePendingApprovalFlow takes it as input | Explicit dependency | |

**User's choice:** Sibling hooks

### Q18a: Sibling-hooks coordination mechanism

| Option | Description | Selected |
|--------|-------------|----------|
| useChat({ onApprovalRequired }) callback prop | Explicit, testable in isolation | ✓ |
| Shared Zustand store for pendingApproval | Decoupled but global state for one slice | |
| useChat returns setPendingApproval; usePendingApprovalFlow accepts it | Two halves of one machine | |

**User's choice:** Callback prop wired by ChatPage

### Q19: ProjectForm 4-tab split

| Option | Description | Selected |
|--------|-------------|----------|
| useProjectForm holds full state; tabs are dumb display | Single source of truth | ✓ |
| Each tab owns its slice via sub-hooks | Risk of validation drift | |
| useProjectForm holds shared state; tabs receive slice props | Middle ground; prop drilling | |

**User's choice:** Full-state useProjectForm + dumb tabs

### Q20: FilterableTable shared API surface

| Option | Description | Selected |
|--------|-------------|----------|
| Generic `<FilterableTable<T>>` typed component | Most flexible; over-generalization risk | |
| `<FilterableTable rows={DataRow[]}>` stringly-typed | Loses type safety | |
| Composition primitives: `<DataTable>` + filter/search hooks | Lowest abstraction cost | ✓ |

**User's choice:** Composition primitives

### Q21: Frontend vs backend ordering

| Option | Description | Selected |
|--------|-------------|----------|
| After backend (per SPEC.md) | Reduces risk of mid-flight convention shifts | |
| In parallel (independent worktree branches) | Faster total wall-clock | ✓ |
| Frontend first | Frees frontend for feature work sooner | |

**User's choice:** Frontend in parallel with backend

---

## Claude's Discretion

The following remain at planner/executor discretion (no user-visible impact):
- Exact file naming inside `chatproxy/`, `wire/`, `pkg/agentbase/`, `handler/oauth/`, `handler/connect/`
- Whether `wire/` is one file or split (e.g. databases.go, repositories.go, services.go, handlers.go)
- Internal layout of `pkg/agentbase/` (one file or split per interface)
- Mocking strategy for `pkg/orchestratorclient/` in service/hitl.go tests
- Visual / interaction details inside the new ProjectForm tabs (no UX changes intended)
- Exact `<DataTable>` prop surface (refine during plan 19-12)

## Deferred Ideas

- Google Business EXPERIMENTAL move (separate trivial PR)
- fx/wire DI library adoption (premature)
- Interface segregation for BusinessService / IntegrationService (out of scope)
- Bug-fix bundling during refactor (file as separate issues)
- "Approve all" shortcut in HITL UX (no UX changes this phase)
- `<DataTable>` migration cadence across 4 list pages (decided during planning)
- Trust-ladder auto-promotion (revisit in v1.4)
