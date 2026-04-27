# Phase 19 Deferred Items

Items discovered during execution but out of scope for the current plan
(per the GSD scope-boundary rule: only auto-fix issues directly caused by
the current task's changes).

## From Plan 19-05 (a11y audit + axe gate)

### Pre-existing prettier failures (out of scope)

`pnpm exec prettier --check .` flags 6 files NOT modified by Plan 19-05:

- `services/frontend/__tests__/highlight-flow.test.tsx`
- `services/frontend/app/(app)/layout.tsx`
- `services/frontend/components/chat/__tests__/ProjectChip.test.tsx`
- `services/frontend/components/sidebar/__tests__/SearchResultRow.test.tsx`
- `services/frontend/hooks/__tests__/useConversations.pin.test.tsx`
- `services/frontend/hooks/useHighlightMessage.ts`

These date back to earlier Phase 19 plans (or earlier phases). They are
NOT caught by `make lint-frontend` because the baseline established by the
preceding phase was already in this state. The fix is `pnpm exec prettier
--write .` — but that touches files outside the 19-05 scope and would
muddle the diff. Recommendation: a follow-up `chore: prettier --write
backlog` plan in 19-06 or v1.4 cleanup.
