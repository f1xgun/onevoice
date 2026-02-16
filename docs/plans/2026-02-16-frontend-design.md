# Frontend Design — OneVoice

**Date:** 2026-02-16
**Status:** Approved

---

## Goal

Build a full Next.js 14 frontend for OneVoice: a Russian-language SMB automation platform. Users interact via a chat interface that dispatches LLM-powered actions to platform agents (VK, Telegram, Yandex.Business).

---

## Tech Stack

| Concern | Choice |
|---------|--------|
| Framework | Next.js 14 (App Router) |
| UI | React 18 + TypeScript |
| Styling | Tailwind CSS + shadcn/ui |
| Data fetching | TanStack Query v5 |
| Global state | Zustand |
| HTTP client | Axios (with interceptor for token refresh) |
| Proxy (dev) | `next.config.js` rewrites |
| Proxy (prod) | Nginx in docker-compose |

---

## Phases

| Phase | Scope |
|-------|-------|
| **5a** | Project scaffold, Next.js config, Nginx, auth (login/register), app shell (sidebar + layout), chat page (SSE + expandable tool cards), integrations page |
| **5b** | Business profile form + operating schedule form |
| **5c** | Landing page, reviews feed, posts history, tasks log, account settings |

---

## Project Structure

```
services/frontend/
├── app/
│   ├── (public)/
│   │   ├── page.tsx              # landing page (Phase 5c)
│   │   ├── login/page.tsx
│   │   └── register/page.tsx
│   ├── (app)/
│   │   ├── layout.tsx            # sidebar + auth guard
│   │   ├── chat/page.tsx
│   │   ├── chat/[id]/page.tsx
│   │   ├── integrations/page.tsx
│   │   ├── business/page.tsx     # Phase 5b
│   │   ├── reviews/page.tsx      # Phase 5c
│   │   ├── posts/page.tsx        # Phase 5c
│   │   ├── tasks/page.tsx        # Phase 5c
│   │   └── settings/page.tsx     # Phase 5c
│   └── layout.tsx                # root layout (fonts, QueryClientProvider, Toaster)
├── components/
│   ├── ui/                       # shadcn/ui generated components
│   ├── chat/
│   │   ├── ChatWindow.tsx        # message list + input
│   │   ├── MessageBubble.tsx     # user / assistant message wrapper
│   │   ├── ToolCallsBlock.tsx    # expandable tool cards inside assistant message
│   │   └── ToolCard.tsx          # single tool action card (platform, status, result)
│   ├── integrations/
│   │   ├── PlatformCard.tsx
│   │   └── ConnectDialog.tsx
│   └── business/
│       ├── ProfileForm.tsx
│       └── ScheduleForm.tsx
├── lib/
│   ├── api.ts                    # axios instance + 401 refresh interceptor
│   ├── auth.ts                   # Zustand store: user, accessToken, refresh()
│   └── query.ts                  # TanStack QueryClient singleton
├── hooks/
│   └── useChat.ts                # SSE streaming hook (fetch + ReadableStream)
├── next.config.js                # rewrites /api/v1/* and /chat/*
└── Dockerfile
```

---

## Authentication

### Token storage
- `accessToken` — Zustand store (in-memory, lost on refresh)
- `refreshToken` — `localStorage` (persists across sessions)

### Session restore on load
On app boot: if `refreshToken` in localStorage → `POST /api/v1/auth/refresh` → populate Zustand store silently.

### Axios interceptor
- On 401 response → attempt token refresh → retry original request once
- On second 401 → clear store + localStorage + redirect to `/login`

### Route protection
`(app)/layout.tsx` checks Zustand auth state; if unauthenticated → `redirect('/login')`.

---

## App Shell

```
┌─────────────┬──────────────────────────────────┐
│  Sidebar    │  Page content                    │
│  (240px)    │                                  │
│             │                                  │
│ Logo        │                                  │
│ ─────────── │                                  │
│ 💬 Чат      │                                  │
│ 🔌 Интеграции│                                 │
│ 🏢 Бизнес   │                                  │
│ ⭐ Отзывы   │                                  │
│ 📝 Посты    │                                  │
│ ⚙ Настройки│                                  │
│             │                                  │
│ ─────────── │                                  │
│ VK  🟢      │                                  │
│ TG  🟢      │                                  │
│ YB  🟡      │                                  │
└─────────────┴──────────────────────────────────┘
```

Platform status dots (bottom of sidebar) fetched once on layout mount via `GET /api/v1/integrations`.

---

## Chat Interface

### SSE streaming (`hooks/useChat.ts`)

```
POST /chat/{conversationId}
  Body: { message: string, model: string, activeIntegrations: string[] }
  Response: text/event-stream
  Events:
    { type: "text", content: "..." }       — stream assistant text
    { type: "tool_call", tool_name: "...", tool_args: {...} }
    { type: "tool_result", tool_name: "...", result: {...}, error?: string }
    { type: "done" }
```

Hook accumulates events into a single `AssistantMessage`:
```typescript
type AssistantMessage = {
  text: string
  toolCalls: { name: string; args: unknown; result?: unknown; error?: string; status: "pending"|"done"|"error" }[]
  status: "streaming" | "done"
}
```

### Expandable tool cards (Approach 3)

Collapsed state (default):
```
▶ Показать действия (2)   ✓ 2/2   [VK] [TG]
```

Expanded state:
```
▼ Скрыть действия (2)   ✓ 2/2   [VK] [TG]
┌────────────────────────────────────────┐
│ [VK]  vk__publish_post            ✅   │
│  "Скидки этой недели..."               │
│  post_id: 12345  [Открыть ↗]          │
└────────────────────────────────────────┘
┌────────────────────────────────────────┐
│ [TG]  telegram__send_channel_post  ✅  │
│  "Скидки этой недели..."               │
└────────────────────────────────────────┘
```

While streaming: spinner in toggle row. Cards update in-place as `tool_result` events arrive.

### Zero state
3 quick-action chips: "Проверить отзывы", "Обновить часы работы", "Опубликовать пост"

---

## Integrations Page

- Grid of platform cards (MVP: VK, Telegram, Yandex.Business; greyed-out: 2GIS, Avito, Google)
- Each card: logo, name, status badge, "Подключить"/"Отключить" button, last sync time
- Connect dialogs:
  - **Telegram**: text input for bot token
  - **VK**: OAuth redirect button
  - **Yandex.Business**: instructions + session cookie JSON field

---

## Business Profile Page (Phase 5b)

Single form, sections:
1. Основная информация: name, category (select), phone, website, description, logo (image upload)
2. Адрес: address string
3. Расписание: 7-day grid — per day: open time / close time / "Выходной" toggle
4. Save button → `PUT /api/v1/business`

---

## Remaining Pages (Phase 5c)

**Reviews:** Feed of cards — platform badge, author, star rating, text, "Ответить с AI" → opens dialog with AI-generated reply (editable before publish)

**Posts / Tasks:** shadcn `DataTable` with filters (platform, status, date range)

**Settings:** Account info form (name, email, password change)

**Landing page:** Static Server Component — Hero, Features (3-col grid), Platforms logos, CTA banner, Footer. Russian copy. No auth required.

---

## Nginx (production docker-compose)

```nginx
location /api/v1/ { proxy_pass http://api:8080; }
location /chat/   { proxy_pass http://orchestrator:8090; }
location /        { proxy_pass http://frontend:3000; }
```

---

## Error Handling

- API errors: TanStack Query `onError` → `toast.error(message)` via shadcn Toaster
- SSE errors: `useChat` hook emits error event → inline error message in chat thread
- Auth errors: Axios interceptor handles 401 globally
- Form validation: react-hook-form + zod schemas

---

## Testing

- Component tests: Vitest + React Testing Library for `ChatWindow`, `ToolCallsBlock`, auth forms
- E2E: not in scope for MVP (thesis deadline)
