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

// Minimal structural type that mirrors the `.raw()` method on next-intl's
// `Translator`. We accept anything with this shape so the factory works
// for both `useTranslations('quickActions')` results and for hand-built
// stubs in tests, without casting through `unknown`.
export type RawTranslator = { raw(key: string): unknown };

export function createDefaultQuickActions(t: RawTranslator): readonly string[] {
  const rawDefaults = t.raw('defaults');
  return Object.freeze(Array.isArray(rawDefaults) ? (rawDefaults as string[]) : []);
}

export function useDefaultQuickActions(): readonly string[] {
  // next-intl's `Translator` already exposes a `.raw()` method (returns
  // `any`), so a plain narrowing cast to `RawTranslator` is sufficient —
  // we no longer route through `unknown`, which would erase the nominal
  // brand on the original translator.
  const t = useTranslations('quickActions') as RawTranslator;
  return useMemo(() => createDefaultQuickActions(t), [t]);
}

export const MAX_QUICK_ACTIONS = 6;
