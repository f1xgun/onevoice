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
├── components/
│   ├── ui/           # shadcn/ui primitives (don't edit manually)
│   └── ...           # Feature components
├── lib/              # Utilities, API client, constants
├── hooks/            # Custom React hooks
└── stores/           # Zustand stores
```

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
