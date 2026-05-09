import { getTranslator } from '@/lib/i18n/translator';

// Defaults are sourced from `quickActions.defaults` in messages/ru.json
// (a JSON array, resolved here via translator.raw at module load). The
// empty-state composer renders these as suggestion chips when a project
// hasn't customized its own list yet.
const tQuickActions = getTranslator('quickActions');
const rawDefaults = tQuickActions.raw('defaults');

export const DEFAULT_QUICK_ACTIONS: readonly string[] = Object.freeze(
  Array.isArray(rawDefaults) ? (rawDefaults as string[]) : []
);

export const MAX_QUICK_ACTIONS = 6;
