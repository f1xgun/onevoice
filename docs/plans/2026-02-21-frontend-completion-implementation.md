# Frontend Completion — Implementation Plan

**Date:** 2026-02-21
**Design:** [2026-02-21-frontend-completion-design.md](2026-02-21-frontend-completion-design.md)
**Parallelism:** 4 independent worktrees

---

## Feature A: Backend APIs (`feat/backend-apis`)

### A1. Domain interfaces + errors
**Files:** `pkg/domain/repository.go`, `pkg/domain/errors.go`
- Add `ReviewRepository`, `PostRepository`, `AgentTaskRepository` interfaces
- Add filter types: `ReviewFilter`, `PostFilter`, `TaskFilter`
- Add sentinel errors: `ErrReviewNotFound`, `ErrPostNotFound`, `ErrAgentTaskNotFound`

### A2. Review MongoDB repository
**File:** `services/api/internal/repository/mongo/review_repo.go` (new)
- `reviewRepository` struct with `*mongo.Collection`
- `ListByBusinessID(ctx, businessID, filter) ([]Review, int, error)` — with platform/reply_status filters
- `GetByID(ctx, id) (*Review, error)`
- `UpdateReply(ctx, id, replyText, replyStatus) error`

### A3. Post MongoDB repository
**File:** `services/api/internal/repository/mongo/post_repo.go` (new)
- `postRepository` struct
- `ListByBusinessID(ctx, businessID, filter) ([]Post, int, error)` — with platform/status filters
- `GetByID(ctx, id) (*Post, error)`

### A4. AgentTask MongoDB repository
**File:** `services/api/internal/repository/mongo/agent_task_repo.go` (new)
- `agentTaskRepository` struct
- `ListByBusinessID(ctx, businessID, filter) ([]AgentTask, int, error)` — with platform/status/type filters

### A5. Review service
**File:** `services/api/internal/service/review_service.go` (new)
- Interface + implementation
- Business ownership verification via BusinessService
- `List`, `GetByID`, `Reply` methods

### A6. Post service
**File:** `services/api/internal/service/post_service.go` (new)
- Interface + implementation
- Business ownership verification
- `List`, `GetByID` methods

### A7. AgentTask service
**File:** `services/api/internal/service/agent_task_service.go` (new)
- Interface + implementation
- `List` method

### A8. Review handler
**File:** `services/api/internal/handler/review_handler.go` (new)
- `ListReviews` — GET /api/v1/reviews
- `GetReview` — GET /api/v1/reviews/{id}
- `ReplyToReview` — PUT /api/v1/reviews/{id}/reply

### A9. Post handler
**File:** `services/api/internal/handler/post_handler.go` (new)
- `ListPosts` — GET /api/v1/posts
- `GetPost` — GET /api/v1/posts/{id}

### A10. AgentTask handler
**File:** `services/api/internal/handler/agent_task_handler.go` (new)
- `ListTasks` — GET /api/v1/tasks

### A11. Password change
**Files:** `services/api/internal/service/user.go`, `services/api/internal/handler/auth.go`
- Add `ChangePassword(ctx, userID, currentPassword, newPassword) error` to UserService
- Add `ChangePassword` handler method

### A12. Route registration + wiring
**Files:** `services/api/internal/router/router.go`, `services/api/cmd/main.go`
- Add new handler fields to `Handlers` struct
- Register new routes in protected group
- Wire repos → services → handlers in main.go

---

## Feature B: Design System (`feat/design-system`)

### B1. Install shadcn components
Run `npx shadcn@latest add` for: table, tabs, skeleton, calendar, popover, textarea, switch, scroll-area, sheet, tooltip, dropdown-menu, alert-dialog

### B2. Color palette refresh
**File:** `services/frontend/app/globals.css`
- Update --primary to indigo (#6366f1 = 239 84% 67%)
- Update --accent, --ring to match
- Keep dark mode consistent

### B3. Mobile-responsive sidebar
**File:** `services/frontend/components/sidebar.tsx`
- Import Sheet from shadcn
- Wrap sidebar in Sheet on mobile (< md)
- Add hamburger button in sticky top bar
- Hide sidebar by default on small screens

---

## Feature C: Form Redesign (`feat/form-redesign`)

### C1. SpecialDate type
**File:** `services/frontend/types/business.ts`
- Add `SpecialDate` interface

### C2. Schedule form redesign
**File:** `services/frontend/components/business/ScheduleForm.tsx`
- Replace raw checkboxes with Switch components
- Add special dates section with calendar popover
- Better visual layout with consistent spacing

### C3. Integrations card redesign
**File:** `services/frontend/components/integrations/PlatformCard.tsx`
- Larger icon (48px)
- ScrollArea for channel list
- AlertDialog for disconnect confirmation
- Better spacing and status indicators

---

## Feature D: Frontend Pages (`feat/frontend-pages`)

### D1. Reviews page
**Files:** `app/(app)/reviews/page.tsx`, new components
- Review cards feed with platform badge, star rating
- Filters: platform, reply status
- AI reply dialog

### D2. Posts page
**Files:** `app/(app)/posts/page.tsx`, new components
- Data table with content preview, platforms, status
- Filters: platform, status
- Expandable row details

### D3. Tasks page
**Files:** `app/(app)/tasks/page.tsx`, new components
- Data table with task type, platform, status, timing
- Filters: platform, status, type
- Expandable details

### D4. Settings page
**Files:** `app/(app)/settings/page.tsx`, new components
- Account info display (name, email, role)
- Password change form with zod validation

### D5. Landing page
**Files:** `app/page.tsx`, new components
- Dark gradient hero, features grid, platforms, how-it-works, CTA, footer
- Auth-aware routing (authenticated → /chat, otherwise → landing)

---

## Merge Order

1. Feature A (backend-apis) — no dependencies
2. Feature B (design-system) — no dependencies
3. Feature C (form-redesign) — benefits from B's shadcn components but can merge independently
4. Feature D (frontend-pages) — benefits from B's components but can merge independently

All features target `main` branch. Resolve any conflicts during merge.
