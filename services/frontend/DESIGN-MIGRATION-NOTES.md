# Linen Design Migration — Working Notes

Working scratchpad for the migration to the Linen design system
(`design_handoff_onevoice 2/`). Delete this file after Phase 5 lands.

## Phase 1 — Foundations · status: complete (compile-time verified)

### Changes

- `app/globals.css` — replaced with Linen tokens (OKLCH).
- `tailwind.config.ts` — rewritten: `var(--token)` instead of `hsl(var(--token))`,
  added direct OneVoice tokens (`paper.*`, `ink.*`, `line.*`, `ochre.*`,
  `success/warning/danger/info`), `boxShadow.ov-1/2/3`, font/radius from tokens.
- `app/layout.tsx` — swapped Inter for `Manrope` + `JetBrains_Mono` via
  `next/font/google`, exposed as `--font-sans` / `--font-mono`.
- `app/fonts/` — removed (Geist .woff files were dead code, no references).

### Font choice — Manrope, not Mona Sans

Design spec calls for **Mona Sans**, but Google Fonts ships it without the
Cyrillic subset (verified: 0 codepoints in U+0400–04FF on `monasans/v4`).
PRODUCTION-README §1 authorises Manrope as the cyrillic fallback. Manrope
ships 104 cyrillic codepoints — full А-Я + extended.

To restore Mona Sans later, self-host from
[github.com/github/mona-sans](https://github.com/github/mona-sans) (the
upstream variable font does include Cyrillic) and load it via
`next/font/local` instead of `next/font/google`.

### Verification

- `pnpm lint` → clean
- `npx tsc --noEmit` → clean
- `pnpm build` → all 16 routes generate, no token errors

### ⚠️ Visual verification deferred — Node v25 incompat

`pnpm dev` returns HTTP 500 on every route with
`TypeError: localStorage.getItem is not a function`. Root cause is Node v25.6.0
shipping a **broken `localStorage` global by default** without a backing file
(see Node warning: `--localstorage-file was provided without a valid path`).
Repro outside this project:

```bash
$ node -e "console.log(typeof localStorage, typeof localStorage?.getItem)"
object undefined
(node:NN) Warning: `--localstorage-file` was provided without a valid path
```

This makes `typeof localStorage !== 'undefined'` checks falsely succeed during
SSR, then `getItem` throws. **Pre-existing, not caused by Phase 1** — the same
500 happens on `main` with Node 25.

Workarounds for visual review:

1. **Pin Node 22 LTS** (recommended) via `nvm use 22` or
   `brew install node@22` and override the symlink.
2. Add a server-side localStorage stub. Drop a `scripts/no-localstorage.js`
   that does `globalThis.localStorage = undefined;` and run dev with
   `NODE_OPTIONS='--require ./scripts/no-localstorage.js' pnpm dev`.

Either workaround is out of scope for the design migration — it should be
fixed in a separate infra commit.

## Open items for Phase 2–4

- **Sidebar (Phase 3)** — current `components/sidebar.tsx` is 240px with
  labels and `bg-gray-900`. Design wants 64px icon-only rail with
  `--ov-paper-raised` background, active = ink icon + 2px ochre left bar,
  tooltips on hover. Nav order also changed: `Чат, Интеграции, Профиль
бизнеса, Отзывы, Посты, Задачи, Настройки`.
- **Button override (Phase 2)** — new `tokens/button.tsx` uses variants
  `primary` / `accent` / `secondary` / `ghost` / `outline` / `danger` / `link`,
  not the current `default` / `destructive` / `outline` / `secondary` /
  `ghost` / `link`. Either alias `default → primary` and `destructive →
danger` in the override or grep-and-replace call sites. **Decision pending.**
- **Inbox / `/inbox` route** — README of v2 explicitly says do not build it.
  Drop it from any nav/menu drafts.
- **Google + 2GIS** — render in "Скоро" / dashed-border section only, never
  as live integrations. Hide entirely if backend doesn't return them.
- **shadcn audit (Phase 2 sweep)** — 23 components beyond the four overrides
  inherit the new colors via shadcn aliases automatically, but radii / sizes
  / focus rings may drift from the mocks. Plan a sweep after the four
  overrides land.
- **`/settings/tools` refactor (Phase 4)** — already exists with
  `ToolsPageClient` + `ToolApprovalToggle`; needs UI refactor to the 4-mode
  segmented + recommended-defaults + drift-detection pattern from
  `mock-settings.jsx`. Bekend contract stays.
