// localStorage helpers for the getting-started checklist and per-section help.
// Per-device, per-business dismiss state — mirrors WhitelistWarningBanner's
// pattern (SSR-guarded read in a useState initializer, try/catch so a locked
// or unavailable storage degrades to "not dismissed" instead of throwing).

const GETTING_STARTED_PREFIX = 'onboarding:gettingStarted:';
const SECTION_HELP_PREFIX = 'help:';

export function gettingStartedDismissKey(businessId: string | null): string {
  return `${GETTING_STARTED_PREFIX}${businessId ?? 'global'}`;
}

export function sectionHelpDismissKey(section: string, businessId: string | null): string {
  return `${SECTION_HELP_PREFIX}${section}:${businessId ?? 'global'}`;
}

export function readDismissed(key: string): boolean {
  if (typeof window === 'undefined') return false;
  try {
    return window.localStorage.getItem(key) === '1';
  } catch {
    return false;
  }
}

export function writeDismissed(key: string): void {
  if (typeof window === 'undefined') return;
  try {
    window.localStorage.setItem(key, '1');
  } catch {
    // no-op: a full or unavailable storage just means the hint re-shows.
  }
}
