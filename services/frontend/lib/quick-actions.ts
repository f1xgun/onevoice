import { useMemo } from 'react';
import { useTranslations } from 'next-intl';

// Quick-action defaults — request-scoped (Phase B1).
//
// Defaults are sourced from `quickActions.defaults` in messages/*.json
// (a JSON array). `useDefaultQuickActions()` returns the array for the
// active locale; the empty-state composer renders the items as
// suggestion chips when a project hasn't customised its own list yet.
//
// `createDefaultQuickActions(t)` is the underlying factory — call from
// server-side code that already has a translator instance.

type RawTranslator = { raw: (key: string) => unknown };

export function createDefaultQuickActions(t: RawTranslator): readonly string[] {
  const rawDefaults = t.raw('defaults');
  return Object.freeze(Array.isArray(rawDefaults) ? (rawDefaults as string[]) : []);
}

export function useDefaultQuickActions(): readonly string[] {
  const t = useTranslations('quickActions') as unknown as RawTranslator;
  return useMemo(() => createDefaultQuickActions(t), [t]);
}

export const MAX_QUICK_ACTIONS = 6;
