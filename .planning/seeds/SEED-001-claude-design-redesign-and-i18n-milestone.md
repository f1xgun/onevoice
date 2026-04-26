---
id: SEED-001
status: dormant
planted: 2026-04-26
planted_during: v1.3 / Phase 17
trigger_when: starting Phase 19 planning OR v1.3 marked complete
scope: Large
---

# SEED-001: Claude Design–driven Phase 19 sidebar + v1.4 "Theming & i18n" milestone

## Why This Matters

The current frontend looks visibly AI-generated (uniform card stacks, default
shadcn shells, no typographic rhythm). Two adjacent gaps make this the right
moment to fix it:

1. **Phase 19 is already a sidebar redesign.** If we wait until v1.4 to do a
   redesign pass, Phase 19 will redesign the sidebar manually first, then v1.4
   will redesign it again — wasted work. Driving Phase 19's redesign through
   Claude Design (https://claude.ai/design — Anthropic Labs research preview,
   Apr 2026) means it gets done once with the new aesthetic baked in.
2. **No theming or i18n today.** `tailwind.config.ts` already maps colors to
   shadcn CSS variables (`hsl(var(--background))` etc.), but `next-themes` is
   not installed and there is no i18n package. All UI strings are hardcoded
   Russian. Backend errors are bare strings, not codes — so localization is a
   cross-stack change, not just a frontend one.

User has a Max subscription, so Claude Design is accessible.

## When to Surface

**Trigger:** Surface this seed during `/gsd:new-milestone` when:

- The new milestone scope mentions any of: design, theme, theming, dark mode,
  light mode, localization, i18n, l10n, translation, redesign, UX polish.
- v1.3 has just been marked complete (the natural moment to open v1.4).

**Also surface during `/gsd:plan-phase` for Phase 19** — its scope is
"Search & Sidebar Redesign", which is the single best moment to fold Claude
Design into the actual redesign rather than redoing it later.

## Scope Estimate

**Large** — full milestone v1.4 plus a Phase 19 plan adjustment.

Proposed v1.4 phases (refine when milestone is opened):

- **A. Theming foundation:** install `next-themes`, audit `services/frontend/`
  for hardcoded color values that bypass `--background`/`--foreground`/etc.
  tokens, add a theme switcher. Independently shippable.
- **B. i18n infrastructure:** `next-intl` on the frontend; on the Go backend,
  switch user-facing errors in `services/api/` and `pkg/domain/` to a
  `{code, params}` shape so the frontend translates them. Server-side
  rendering for agent-emitted user messages (Telegram DMs etc.) uses
  `golang.org/x/text/message` keyed by user locale.
- **C. RU/EN string extraction:** mechanical pass through screens and
  backend error sites to wrap strings.
- **D. Redesign pass via Claude Design:** prototype remaining screens not
  touched by Phase 19 (chat view, settings, project pages, integrations,
  approval card). Apply handoff bundles via Claude Code.

Languages: **RU + EN only** (confirmed 2026-04-26).

## Phase 19 Adjustment (when seed surfaces during Phase 19 planning)

Reshape the Phase 19 plan so the sidebar redesign + search UI go through
Claude Design instead of being designed manually:

1. Before planning, user prototypes the master/detail sidebar + pinned chats +
   search in Claude Design (it can read `services/frontend/` + shadcn config
   to consume the existing design system).
2. User iterates via conversation/comments/sliders inside Claude Design until
   the layout is right.
3. Claude Design exports a handoff bundle.
4. Phase 19 plan task: apply handoff to `services/frontend/components/`,
   wiring it into `app/(app)/layout.tsx` and the sidebar component tree.

Caveats to flag in the plan:

- Claude Design is research preview — handoff format may be raw HTML/Tailwind
  rather than shadcn components. Expect a translation pass to map back to the
  project's component API.
- Theme tokens should be cleaned up (or at least audited) **before** running
  Claude Design, otherwise it sees hardcoded colors as "the design system" and
  reproduces the AI-generated look.

## Breadcrumbs

Frontend layout / sidebar (Phase 19 will rewrite):

- `services/frontend/components/sidebar.tsx`
- `services/frontend/components/__tests__/sidebar.test.tsx`
- `services/frontend/app/(app)/layout.tsx`
- `services/frontend/app/(app)/projects/[id]/chats/page.tsx`

Theming foundation (already in place — tokens exist, no theme switcher yet):

- `services/frontend/tailwind.config.ts` — colors already mapped to CSS vars
- `services/frontend/components.json` — shadcn config
- `services/frontend/package.json` — does NOT include `next-themes` or any
  i18n package as of 2026-04-26

Backend strings that need to become error codes for i18n:

- `services/api/` — all handler error responses
- `pkg/domain/` — domain validation errors

References to `/docs/api-design.md` and `/docs/security.md` will need updates
once the error-code contract changes.

## Notes

- shadcn ships an official "skill" package (https://ui.shadcn.com/docs/skills)
  that gives LLMs current component patterns. Worth installing into Claude
  Code before the redesign pass so generated code uses up-to-date shadcn API.
- Don't "do all of this in parallel with Phase 18/19" — the redesign in Phase
  19 will collide with theming work. Wait until v1.3 is shipped before
  opening v1.4.
- If Claude Design output disappoints on a small trial run during Phase 19,
  keep the v1.4 milestone but drop Phase D (the broader redesign pass) and
  ship just theming + i18n.
