# services/frontend/ — Next.js Dashboard

User-facing dashboard for managing businesses, integrations, and AI chat.

**Port:** 3000

## Stack

- Next.js 14 (App Router)
- React 18
- TypeScript (strict mode)
- Tailwind CSS 3 + shadcn/ui
- Zustand (global state)
- TanStack React Query (server state)
- react-hook-form + zod (forms)
- Vitest + Testing Library (tests)

## Project Structure

```
src/
├── app/              # Next.js App Router pages
│   ├── (auth)/       # Auth route group (login, register)
│   └── (dashboard)/  # Dashboard route group
│       └── posts/_components/  # Page-scoped split-out components for posts page
├── components/
│   ├── ui/           # shadcn/ui primitives (don't edit manually)
│   ├── lists/
│   │   └── DataTable.tsx        # Composition primitive used by list pages
│   ├── projects/
│   │   ├── ProjectForm.tsx      # Thin shell that wires useProjectForm + tabs
│   │   ├── useProjectForm.ts    # Single source of truth (react-hook-form schema)
│   │   ├── BasicsTab.tsx        # "Основное" tab fields
│   │   ├── PromptTab.tsx        # "Промпт" tab fields
│   │   ├── ToolsTab.tsx         # "Инструменты" tab fields
│   │   └── QuickActionsTab.tsx  # "Быстрые действия" tab fields
│   └── ...           # Other feature components
├── lib/
│   ├── sse.ts        # Pure SSE helpers (parseSSELine, applySSEEvent) — shared by chat hooks
│   └── ...           # Other utilities, API client, constants
├── hooks/
│   ├── useChat.ts                  # Message[] + SSE driver; accepts onApprovalRequired callback
│   ├── usePendingApprovalFlow.ts   # HITL approval state + resolveApproval/resume stream
│   ├── useDataTableFilters.ts      # Filter primitive composed by list pages
│   ├── useDataTableSearch.ts       # Search primitive composed by list pages
│   └── ...                         # Other custom React hooks
└── stores/           # Zustand stores
```

**Composition note:** `useChat` and `usePendingApprovalFlow` are sibling hooks consumed in
parallel by `ChatPage` (D-19 of Phase 19). `<DataTable>` + the two `useDataTable*` hooks are
composition primitives — list pages (tasks, posts, integrations, reviews) compose them
locally rather than wrapping a monolithic table component (D-21).

## Rules

- **Server components by default.** Add `"use client"` only when using hooks/events.
- **Tailwind only.** No inline styles, no CSS modules.
- **Forms:** Always react-hook-form + zod. Never manual `useState` for form fields.
- **State:** Zustand for global (auth, integrations). React Query for server data. `useState` for local UI only.
- **Components:** `function` declarations (not arrow), typed props interfaces.
- **Type imports:** Use `import type { ... }` for type-only imports.

## API Proxy

`next.config.js` rewrites:

- `/api/v1/*` → API service (`:8080`)
- `/chat/*` → Orchestrator service (`:8090`)

## Build & Test

```bash
cd services/frontend && pnpm lint          # ESLint
cd services/frontend && pnpm exec prettier --check .  # Prettier
cd services/frontend && pnpm test          # Vitest
cd services/frontend && pnpm build         # Production build
```
