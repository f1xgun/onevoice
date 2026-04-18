# Frontend Style — OneVoice Dashboard

Detailed rules for `services/frontend/` (Next.js 14, React 18, TypeScript, Tailwind, shadcn/ui, pnpm).

For scope-specific quick rules see [../services/frontend/AGENTS.md](../services/frontend/AGENTS.md).
For concrete good/bad snippets see [patterns.md](patterns.md) and [anti-patterns.md](anti-patterns.md).

---

## Project Structure

```
src/
├── app/              # Next.js App Router pages
│   ├── (auth)/       # Auth route group (login, register)
│   └── (dashboard)/  # Dashboard route group
├── components/
│   ├── ui/           # shadcn/ui primitives — DO NOT edit manually
│   └── ...           # Feature components
├── lib/              # Utilities, API client, constants
├── hooks/            # Custom React hooks
└── stores/           # Zustand stores
```

## Component Patterns

- Always define TypeScript interfaces for props; never `props: any` or untyped destructures.
- Use `function` declarations, **not** `const` arrow functions — better stack traces and debugging.
- Server Components by default; add `"use client"` only when you need hooks or event handlers.
- Tailwind classes only — no inline `style={{...}}`, no CSS modules, no `styled-components`.
- Extract logic to custom hooks when a component grows past ~10 lines of logic.

## State Management

- **Zustand** — global state (auth, integrations status). One store per logical domain.
- **TanStack React Query** — server state (fetching, caching, invalidation).
- **`useState`** — *only* for local UI state (open/closed, selected tab, input focus).
- Never lift state unnecessarily. Keep state as close as possible to where it's used.
- Never duplicate Zustand state in `useState` — one is always wrong.

## Forms

- Every form uses `react-hook-form` + `zod`.
- Define the schema first, infer TypeScript types: `type FormData = z.infer<typeof schema>`.
- Show validation errors **inline**, never via `alert()`.
- Handle loading and error states explicitly in the UI.

## Type Imports

- Use `import type { Foo } from '...'` for type-only imports (ESLint rule `@typescript-eslint/consistent-type-imports`).
- Group imports: external → internal absolute (`@/...`) → relative.

## Styling Rules

- Tailwind utility classes grouped logically (layout → spacing → typography → color).
- shadcn/ui primitives live in `components/ui/` — do not hand-edit them; re-run the CLI to regenerate.
- Prefer design tokens (`text-muted-foreground`, `bg-background`) over raw color values.

## Build / Test Commands

```bash
cd services/frontend
pnpm lint                       # ESLint
pnpm exec prettier --check .    # Prettier
pnpm test                       # Vitest
pnpm build                      # Production build
pnpm dev                        # Local dev server (port 3000)
```

## API Proxy

`next.config.js` rewrites:

- `/api/v1/*` → API service (`:8080`)
- `/chat/*` → Orchestrator service (`:8090`)

Use these paths from the frontend; never hardcode `http://localhost:8080` in component code.

## Dependencies

- `next@14`
- `react@18`
- `tailwindcss@3`
- `shadcn/ui`
- `zustand` — state
- `@tanstack/react-query` — server state
- `react-hook-form` + `zod` — forms

Before adding a new dependency, check whether shadcn/ui or an existing hook already covers it.

## References

- [React Best Practices](https://react.dev/learn)
- [Next.js Documentation](https://nextjs.org/docs)
- [Zustand](https://docs.pmnd.rs/zustand/getting-started/introduction)
- [TanStack Query](https://tanstack.com/query/latest/docs/framework/react/overview)
