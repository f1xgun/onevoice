# OneVoice Development Workflow

This document defines the **mandatory** development workflow for OneVoice implementation. These are not suggestions - they must be followed for all feature work.

## The Basic Workflow

All feature development follows this ordered sequence:

### 1. 🎨 brainstorming
**Activates:** Before writing any code
**Purpose:** Refines rough ideas through questions, explores alternatives, presents design in sections for validation
**Output:** Saves design document to `docs/plans/YYYY-MM-DD-<feature>-design.md`

**Usage:**
```
User: "I want to add user authentication"
Agent: [Uses /brainstorming skill]
  → Asks questions about requirements
  → Proposes 2-3 approaches with tradeoffs
  → Presents design in 200-300 word sections
  → Gets validation after each section
  → Saves approved design document
```

---

### 2. 🌳 using-git-worktrees
**Activates:** After design approval
**Purpose:** Creates isolated workspace on new branch, runs project setup, verifies clean test baseline
**Output:** New worktree at `.worktrees/<feature-name>` on branch `feature/<feature-name>`

**Usage:**
```
Agent: [Uses /using-git-worktrees skill]
  → Creates .worktrees/<feature> directory
  → Checks out new branch
  → Runs npm install / go mod download / etc
  → Verifies tests pass (clean baseline)
  → Reports: "Worktree ready at <path>"
```

**Critical:** No code changes on main branch - all work in worktrees.

---

### 3. 📋 writing-plans
**Activates:** With approved design
**Purpose:** Breaks work into bite-sized tasks (2-5 minutes each)
**Output:** Saves plan to `docs/plans/YYYY-MM-DD-<feature>-implementation.md`

**Task Structure:**
- **Exact file paths** (create/modify/test)
- **Complete code** (not "add validation" - show the actual code)
- **Verification steps** (run command, expected output)
- **Commit message**

**Usage:**
```
Agent: [Uses /writing-plans skill]
  → Reads design document
  → Breaks into 2-5 minute tasks
  → Each task: files → code → test → verify → commit
  → Saves plan document
  → Offers: "Execute with subagent-driven or executing-plans?"
```

---

### 4. 🤖 subagent-driven-development OR executing-plans
**Activates:** With plan
**Purpose:** Executes tasks with quality gates

**Option A: subagent-driven-development** (Same session)
- Dispatches fresh subagent per task
- Two-stage review after each task:
  1. **Spec compliance** review (matches plan?)
  2. **Code quality** review (well-built?)
- Fix issues before next task
- Stay in current session

**Option B: executing-plans** (Parallel session)
- Executes in batches (3 tasks)
- Human checkpoints between batches
- Separate Claude Code session

**Usage:**
```
Agent: [Uses /subagent-driven-development]
  → Task 1: Dispatch implementer → Spec review → Quality review → Fix → Done
  → Task 2: Dispatch implementer → Spec review → Quality review → Fix → Done
  → ...
  → All tasks complete
```

---

### 5. 🧪 test-driven-development
**Activates:** During implementation (within each task)
**Purpose:** Enforces RED-GREEN-REFACTOR cycle
**Rule:** Deletes any code written before tests

**Cycle:**
1. **RED:** Write failing test
2. **Verify RED:** Run test, confirm it fails
3. **GREEN:** Write minimal code to pass
4. **Verify GREEN:** Run test, confirm it passes
5. **Refactor:** Clean up (optional)
6. **Commit**

**Usage:**
```
Task: "Add user validation"

Step 1: Write test (RED)
  → Create user_test.go
  → Test: ValidateUser() returns error for empty email
  → Run: go test -v → FAIL (ValidateUser undefined)

Step 2: Implement (GREEN)
  → Create user.go
  → Implement ValidateUser()
  → Run: go test -v → PASS

Step 3: Commit
  → git add user.go user_test.go
  → git commit -m "feat: add user validation"
```

**Critical:** If code exists without tests, DELETE IT and rewrite with TDD.

---

### 6. 🔍 requesting-code-review
**Activates:** Between tasks (after each task in subagent-driven)
**Purpose:** Reviews against plan, reports issues by severity
**Rule:** Critical issues block progress

**Usage:**
```
Agent: [After Task 2 completes]
  → Get git SHAs (BASE_SHA, HEAD_SHA)
  → Dispatch superpowers:code-reviewer subagent
  → Review returns:
    - Critical issues (MUST fix before proceeding)
    - Important issues (SHOULD fix)
    - Minor issues (CAN defer)
  → Fix Critical issues immediately
  → Continue to Task 3
```

**Severity Levels:**
- **Critical:** Blocks compilation, breaks tests, security vulnerability
- **Important:** Architecture violation, missing error handling, poor testability
- **Minor:** Style issues, verbose messages, optimization opportunities

---

### 6.5. 🎨 ui-review + 🧪 playwright-smoke (frontend tasks only)
**Activates:** After code review, when the task modified files under `services/frontend/`
**Purpose:** Visual UI/UX verification using Playwright MCP at three viewports + functional smoke test of the changed feature
**Rule:** Critical issues block progress (same as code review)

**Trigger condition:** Task touched any file matching `services/frontend/**/*.{tsx,ts,css}`. If no frontend files changed, skip this step entirely.

**Prerequisites:** Frontend dev server running at `http://localhost:3000`. Start with `cd services/frontend && pnpm dev` if not running.

**IMPORTANT — Restart before review:** The dev server MUST be restarted before every UI review to guarantee the latest code changes are served. Next.js HMR can miss certain changes (layout shifts, new CSS classes, config updates). Always:
1. Kill the running dev server (`pkill -f 'next dev'` or Ctrl-C the terminal)
2. Restart it (`cd services/frontend && pnpm dev`)
3. Wait for "Ready" message before proceeding

**Usage:**
```
Agent: [After code review passes, frontend files were changed]
  → Restart frontend dev server (kill + pnpm dev)
  → Wait for "Ready" confirmation
  → Dispatch ui-reviewer subagent (read .claude/agents/ui-reviewer.md)
  → Subagent navigates to affected pages via Playwright MCP
  → Screenshots at 3 viewports: desktop (1440x900), tablet (768x1024), mobile (375x812)
  → Accessibility snapshot for each page
  → Console error check
  → Review returns:
    - Critical issues (MUST fix — broken layout, unreadable text, overflow)
    - Important issues (SHOULD fix — spacing inconsistency, poor responsive behavior)
    - Minor issues (CAN defer — small alignment, polish)
  → Fix Critical issues immediately
  → THEN run Playwright smoke test for the changed feature (see below)
  → Continue to next task
```

**Slash command:** `/ui-review` (can also pass specific routes: `/ui-review /integrations /business`)

#### Playwright Smoke Test (mandatory after every change)
After the visual UI review, **always** run a functional smoke test using Playwright MCP directly in the main session:

1. Navigate to the affected page(s)
2. Exercise the primary user flow (e.g. for posts: load page → verify rows render → click row to expand → test filter tabs)
3. Check network requests — confirm API calls return 200 and correct data shape
4. Verify the actual data appears in the UI (not just that the page loads)

**Common pitfalls to catch:**
- API response shape mismatch (e.g. `r.data` vs `r.data.posts` — wrap in correct field)
- Empty state vs loading state vs data state rendering logic
- Filter/tab interactions triggering correct query params
- Expanded/collapsed row state toggling

```
Agent: [After ui-reviewer completes]
  → Use Playwright MCP tools directly (browser_navigate, browser_snapshot, browser_click)
  → Log in if needed (use test credentials from test/integration/auth_test.go)
  → Seed test data via API if the feature requires existing records
  → Walk through the key interactions for the changed feature
  → Confirm data renders correctly, no console errors
  → Report pass/fail — fix any functional issues before merging
```

---

### 7. ✅ finishing-a-development-branch
**Activates:** When all tasks complete
**Purpose:** Verifies tests, presents options, cleans up worktree

**Usage:**
```
Agent: [After all tasks done]
  → Run full test suite
  → Check lint
  → Present options:
    1. Merge to main (if simple)
    2. Create PR (if needs review)
    3. Keep worktree (continue later)
    4. Discard (abandon work)
  → Clean up worktree if chosen
```

---

## Workflow Diagram

```
Rough Idea
    ↓
[brainstorming] → Design Document
    ↓
[using-git-worktrees] → Isolated Workspace
    ↓
[writing-plans] → Implementation Plan (bite-sized tasks)
    ↓
[subagent-driven-development OR executing-plans]
    ↓
    For each task:
        ├─ [test-driven-development] (RED-GREEN-REFACTOR)
        ├─ Implement
        ├─ [requesting-code-review]
        ├─ [ui-review + playwright-smoke] (if frontend files changed)
        ├─ Fix issues
        └─ Next task
    ↓
[finishing-a-development-branch] → Merge/PR/Keep/Discard
```

---

## Mandatory Checkpoints

### Before Writing Code:
- ✅ Design document created via brainstorming
- ✅ Design validated by user
- ✅ Worktree created via using-git-worktrees
- ✅ Implementation plan created via writing-plans

### During Implementation:
- ✅ Every task follows TDD (RED-GREEN-REFACTOR)
- ✅ Code review after each task
- ✅ UI review after each frontend task (Playwright MCP, 3 viewports)
- ✅ Playwright smoke test after each frontend task (functional flow verification)
- ✅ Critical issues fixed immediately

### Before Merging:
- ✅ All tests pass
- ✅ Lint passes
- ✅ Final code review complete
- ✅ User approval for merge

---

## Skill Invocation

Agent **automatically checks** for relevant skills before any task. Skills are invoked with:

```
/brainstorming
/using-git-worktrees
/writing-plans
/subagent-driven-development
/test-driven-development
/requesting-code-review
/ui-review
/finishing-a-development-branch
```

Or programmatically via `Skill` tool.

---

## Current Project Status

**Phase 1 Backend Implementation:**
- ✅ brainstorming → Design created (2026-02-09-phase1-backend-design.md)
- ✅ using-git-worktrees → Worktree created (.worktrees/phase1-backend)
- ✅ writing-plans → Plan created (2026-02-09-phase1-backend-implementation.md)
- ✅ subagent-driven-development → Tasks 1-4 complete
- ✅ requesting-code-review → Code review complete, issues fixed
- 🔄 **Current:** Continuing with Tasks 5-16

**Next:** Task 5 (MongoDB Repositories) following TDD and code review workflow.

---

## Notes

- These workflows are **mandatory**, not optional
- Agent will refuse to proceed without proper workflow
- Each skill builds on the previous (no skipping)
- Code review is non-negotiable between tasks
- TDD is enforced (code without tests = delete and rewrite)

**This is the way.**
