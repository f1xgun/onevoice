// Backend error-code → render contract.
//
// Mirror of services/api/internal/handler/error_mapping.go for code→shape;
// the localized strings live in services/frontend/messages/{ru,en}.json
// under the i18nKey. Sibling plans 21-03 and 21-04 EXTEND the COPY map.
// Do not change the type shape — it is fixed by 21-CROSS-PLAN-CONTRACTS.md §4.

export type ErrorType = 'toast' | 'modal' | 'inline' | 'banner';
export type ToastTone = 'destructive' | 'warning' | 'info';

/**
 * Discriminator-free entry shape — every caller accesses these fields:
 *   i18nKey      → message catalog path (required, used with next-intl `t(...)`)
 *   type         → render hint (default: 'toast')
 *   toastTone    → tone if type='toast' (default: 'destructive')
 *   actionLabel  → optional CTA i18n key (e.g. "auth.passwordReset.errors.requestNewCta")
 *   actionHref   → optional CTA route ("/auth/password-reset", "/settings/account", …)
 */
export type ErrorEntry = {
  i18nKey: string;
  type?: ErrorType;
  toastTone?: ToastTone;
  actionLabel?: string;
  actionHref?: string;
};

/**
 * COPY is the union of all backend error codes Phase 21 surfaces.
 * 21-02 owns: reset_token_invalid, reset_token_expired, password_too_weak.
 * 21-03 and 21-04 will append their own codes (verify_*, sole_owner_*, etc.)
 * — do NOT pre-add them here; that's 21-03's / 21-04's deliverable.
 */
export const COPY: Record<string, ErrorEntry> = {
  reset_token_invalid: {
    i18nKey: 'auth.passwordReset.errors.invalid',
    type: 'inline',
  },
  reset_token_expired: {
    i18nKey: 'auth.passwordReset.errors.expired',
    type: 'inline',
    actionLabel: 'auth.passwordReset.errors.requestNewCta',
    actionHref: '/auth/password-reset',
  },
  password_too_weak: {
    i18nKey: 'auth.passwordReset.errors.weakPassword',
    type: 'inline',
  },
};

const FALLBACK: ErrorEntry = {
  i18nKey: 'errors.generic',
  type: 'toast',
  toastTone: 'destructive',
};

/**
 * Resolves a backend error code to its render entry. Callers then localize
 * via next-intl:
 *   const entry = mapErrorCode(error.code);
 *   const message = t(entry.i18nKey);
 *   // render based on entry.type / entry.toastTone / entry.actionLabel
 */
export function mapErrorCode(code: string | undefined): ErrorEntry {
  return (code && COPY[code]) || FALLBACK;
}

export const ERROR_CODES = Object.keys(COPY) as ReadonlyArray<string>;
