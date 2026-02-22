# Frontend Completion & Design Refresh — OneVoice

**Date:** 2026-02-21
**Status:** Approved

---

## Goal

Complete all stub pages (Reviews, Posts, Tasks, Settings), add a landing page, redesign the schedule form and integrations card, and refresh the overall visual design to a modern indigo/blue SaaS aesthetic (Linear/Vercel style).

---

## Design Decisions

| Decision | Choice |
|----------|--------|
| Color palette | Indigo/Blue (#6366f1 primary) |
| Backend API scope | Read-only + reply for reviews; read-only for posts/tasks |
| Landing page style | Minimal dark hero + white content sections |
| Password change flow | Current password + new password (secure) |

---

## A. Design System Refresh

### Color Palette — Indigo/Blue SaaS Theme

Replacing neutral gray HSL palette with indigo-primary scheme:

| Variable | Current (neutral) | New (indigo) |
|---|---|---|
| `--primary` | `0 0% 9%` | `239 84% 67%` (#6366f1) |
| `--primary-foreground` | `0 0% 98%` | `0 0% 100%` |
| `--accent` | `0 0% 96.1%` | `239 84% 97%` (light indigo tint) |
| `--ring` | `0 0% 3.9%` | `239 84% 67%` |

Sidebar stays `bg-gray-900`. Cards get subtle `shadow-sm`.

### New shadcn Components

Install 11 components: table, tabs, skeleton, calendar, popover, textarea, switch, scroll-area, sheet, tooltip, dropdown-menu.

### Consistent Page Layout

```tsx
<div className="p-6 lg:p-8 space-y-6">
  <div className="flex items-center justify-between">
    <div>
      <h1 className="text-2xl font-semibold tracking-tight">Page Title</h1>
      <p className="text-sm text-muted-foreground">Description</p>
    </div>
    {/* Action buttons */}
  </div>
  {/* Page content */}
</div>
```

### Mobile Sidebar

Sheet component wrapping sidebar on `< md` breakpoints. Hamburger button in sticky top bar. Sidebar hidden by default on mobile.

### Skeleton Loading

Every data-fetching page gets skeleton placeholders matching final layout shape.

---

## B. Schedule Redesign

### Current Issues
- Raw HTML checkboxes
- No special dates support
- Cramped layout

### Proposed Layout

```
┌─────────────────────────────────────────────┐
│ Расписание                                  │
├─────────────────────────────────────────────┤
│ Пн   [Switch: Открыто]   09:00 — 21:00     │
│ Вт   [Switch: Открыто]   09:00 — 21:00     │
│ ...                                         │
│ Вс   [Switch: Выходной]  ───────────────    │
├─────────────────────────────────────────────┤
│ Особые даты                                 │
│ ┌──────────────────────────────────────┐    │
│ │ 08.03 — Выходной            [✕]     │    │
│ │ 31.12 — 10:00-15:00         [✕]     │    │
│ └──────────────────────────────────────┘    │
│ [+ Добавить особую дату]  (Calendar popover)│
└─────────────────────────────────────────────┘
```

### Frontend Types

```typescript
export interface SpecialDate {
  date: string;      // "2026-03-08" ISO format
  open?: string;     // "10:00" — if absent, means closed
  close?: string;    // "15:00"
  closed: boolean;
}
```

### Backend

Verify `PUT /business/schedule` handles `SpecialDate` field in `BusinessSchedule` model. Minor fix if handler ignores it.

---

## C. Integrations Card Redesign

### Changes
- Larger icon area (48px instead of 40px)
- Better spacing between channels
- ScrollArea if > 3 channels
- AlertDialog confirmation before disconnect
- Status dot inline with channel name

### Layout

```
┌───────────────────────────────────────────┐
│  [Icon 48px]  Telegram       🟢 Подключено│
│               Бот для каналов             │
├───────────────────────────────────────────┤
│  Каналы:                                  │
│  ┌─────────────────────────────────────┐  │
│  │ 🟢 Мой канал              [Откл.]  │  │
│  │ 🟢 Второй канал           [Откл.]  │  │
│  └─────────────────────────────────────┘  │
│                                           │
│  [+ Добавить канал]                       │
└───────────────────────────────────────────┘
```

---

## D. Reviews Page (Full Stack)

### Backend

**Repository interface** (`pkg/domain/repository.go`):
```go
ReviewRepository interface {
    ListByBusinessID(ctx context.Context, businessID string, filter ReviewFilter) ([]Review, int, error)
    GetByID(ctx context.Context, id string) (*Review, error)
    UpdateReply(ctx context.Context, id string, replyText string, replyStatus string) error
}

type ReviewFilter struct {
    Platform    string
    ReplyStatus string
    Limit       int
    Offset      int
}
```

**Routes:**
```
GET  /api/v1/reviews              — list (query: platform, reply_status, limit, offset)
GET  /api/v1/reviews/{id}         — detail
PUT  /api/v1/reviews/{id}/reply   — submit reply text
```

**Implementation files:**
- `services/api/internal/repository/mongo/review_repo.go`
- `services/api/internal/service/review_service.go`
- `services/api/internal/handler/review_handler.go`

### Frontend

```
┌──────────────────────────────────────────────────┐
│ Отзывы                                           │
│ Управление отзывами с разных платформ             │
│                                                  │
│ [Все] [Без ответа] [Отвечено] [Ошибка]   📱 VK ▾│
├──────────────────────────────────────────────────┤
│ ┌──────────────────────────────────────────────┐ │
│ │ [VK]  ★★★★☆  Иван Петров   2 дня назад     │ │
│ │ "Отличное обслуживание, рекомендую!"        │ │
│ │ Ответ: Нет ответа                           │ │
│ │ [Ответить с AI]                              │ │
│ └──────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────┘
```

"Ответить с AI" opens dialog with editable AI-suggested reply. Submit via `PUT /reviews/{id}/reply`.

---

## E. Posts Page (Full Stack)

### Backend

**Repository interface:**
```go
PostRepository interface {
    ListByBusinessID(ctx context.Context, businessID string, filter PostFilter) ([]Post, int, error)
    GetByID(ctx context.Context, id string) (*Post, error)
}

type PostFilter struct {
    Platform string
    Status   string
    Limit    int
    Offset   int
}
```

**Routes:**
```
GET  /api/v1/posts        — list (query: platform, status, limit, offset)
GET  /api/v1/posts/{id}   — detail
```

### Frontend — Data Table

```
┌──────────────────────────────────────────────────┐
│ Посты                                            │
│ История публикаций                               │
│                                                  │
│ [Все] [Опубликовано] [Черновик] [Ошибка]  📱 VK ▾│
├──────────────────────────────────────────────────┤
│ Содержание        │ Платформы │ Статус │ Дата    │
│ Скидки этой неде… │ VK TG     │ ✅     │ 20.02  │
│ Новое меню на ве… │ VK        │ ❌     │ 19.02  │
└──────────────────────────────────────────────────┘
```

Expandable rows show full content, per-platform results, media previews.

---

## F. Tasks Page (Full Stack)

### Backend

**Repository interface:**
```go
AgentTaskRepository interface {
    ListByBusinessID(ctx context.Context, businessID string, filter TaskFilter) ([]AgentTask, int, error)
}

type TaskFilter struct {
    Platform string
    Status   string
    Type     string
    Limit    int
    Offset   int
}
```

**Routes:**
```
GET  /api/v1/tasks    — list (query: platform, status, type, limit, offset)
```

### Frontend — Data Table

```
┌──────────────────────────────────────────────────┐
│ Задачи                                           │
│ Журнал действий агентов                          │
│                                                  │
│ [Все] [Выполняется] [Готово] [Ошибка]     📱 VK ▾│
├──────────────────────────────────────────────────┤
│ Тип               │ Платформа │ Статус │ Время   │
│ send_channel_post │ TG        │ ✅     │ 14:23  │
│ publish_post      │ VK        │ ❌     │ 14:20  │
│ fetch_reviews     │ YB        │ ⏳     │ 14:18  │
└──────────────────────────────────────────────────┘
```

Expandable rows: input/output JSON, error details, timing.

---

## G. Settings Page

### Backend

New endpoint: `PUT /api/v1/auth/password`
- Request: `{ currentPassword: string, newPassword: string }`
- Validates current password via bcrypt
- Updates password hash

### Frontend

```
┌──────────────────────────────────────────────────┐
│ Настройки                                        │
│ Управление аккаунтом                             │
│                                                  │
│ ┌──── Аккаунт ──────────────────────────────────┐│
│ │ Имя:    Иван Петров                           ││
│ │ Email:  ivan@example.com                      ││
│ │ Роль:   Владелец                              ││
│ └───────────────────────────────────────────────┘│
│                                                  │
│ ┌──── Смена пароля ─────────────────────────────┐│
│ │ Текущий пароль:   [............]              ││
│ │ Новый пароль:     [............]              ││
│ │ Подтверждение:    [............]              ││
│ │                          [Сменить пароль]     ││
│ └───────────────────────────────────────────────┘│
└──────────────────────────────────────────────────┘
```

---

## H. Landing Page

Static Server Component, Russian copy, no auth required.

```
┌──────────────────────────────────────────────────┐
│ [dark gradient hero: gray-900 → indigo-950]      │
│                                                  │
│         OneVoice                                 │
│  Управляйте цифровым присутствием               │
│  вашего бизнеса с помощью AI                     │
│                                                  │
│  [Начать бесплатно]    [Войти]                   │
│                                                  │
├──────────────────────────────────────────────────┤
│ [white sections]                                 │
│                                                  │
│  Features: AI Чат | Мультиплатформа | Отзывы    │
│  Platforms: VK, Telegram, Яндекс.Бизнес         │
│  How it works: Подключите → Опишите → Готово     │
│  CTA Banner + Footer                            │
│                                                  │
└──────────────────────────────────────────────────┘
```

**Root routing:** `app/page.tsx` checks auth — authenticated → `/chat`, otherwise → landing.

---

## Key Files

### Frontend (modify existing)
- `services/frontend/app/globals.css` — color palette
- `services/frontend/components/sidebar.tsx` — mobile responsive
- `services/frontend/components/business/ScheduleForm.tsx` — redesign
- `services/frontend/components/integrations/PlatformCard.tsx` — redesign
- `services/frontend/types/business.ts` — SpecialDate type

### Frontend (stub → full)
- `services/frontend/app/(app)/reviews/page.tsx`
- `services/frontend/app/(app)/posts/page.tsx`
- `services/frontend/app/(app)/tasks/page.tsx`
- `services/frontend/app/(app)/settings/page.tsx`
- `services/frontend/app/page.tsx` — landing page

### Backend (new files)
- `pkg/domain/repository.go` — add interfaces
- `services/api/internal/repository/mongo/review_repo.go`
- `services/api/internal/repository/mongo/post_repo.go`
- `services/api/internal/repository/mongo/agent_task_repo.go`
- `services/api/internal/service/review_service.go`
- `services/api/internal/service/post_service.go`
- `services/api/internal/service/agent_task_service.go`
- `services/api/internal/handler/review_handler.go`
- `services/api/internal/handler/post_handler.go`
- `services/api/internal/handler/agent_task_handler.go`
- `services/api/internal/router/router.go` — register routes

### Backend (modify existing)
- `services/api/internal/handler/auth_handler.go` — password change
- `services/api/internal/service/user_service.go` — password change
- `services/api/cmd/main.go` — wire new dependencies
