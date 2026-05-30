// Backend error-code → render contract.
//
// Mirror of services/api/internal/handler/error_mapping.go for code→shape;
// the localized strings live in services/frontend/messages/{ru,en}.json
// under the i18nKey. Sibling plans 21-03 and 21-04 EXTEND the COPY map.
// Do not change the type shape — it is fixed by.

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
 * COPY is the union of all backend error codes surfaces.
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

  // email verification + soft-restrict.
  // The i18n keys live under
  // auth.verifyEmail.errors.* in messages/{ru,en}.json. Do not change
  // the entry shape — fixed by §4.
  verify_token_invalid: {
    i18nKey: 'auth.verifyEmail.errors.invalid',
    type: 'inline',
  },
  verify_token_expired: {
    i18nKey: 'auth.verifyEmail.errors.expired',
    type: 'inline',
    actionLabel: 'auth.verifyEmail.errors.resendCta',
  },
  verify_resend_throttled: {
    i18nKey: 'auth.verifyEmail.errors.throttled',
    type: 'toast',
    toastTone: 'warning',
  },
  email_verification_required: {
    // 412 from RequireVerifiedEmailDay0/Day7 — the global modal-host
    // listens for this entry's type==='modal' and renders
    // EmailVerifiedRequiredModal.
    i18nKey: 'auth.verifyEmail.errors.required',
    type: 'modal',
    actionLabel: 'auth.verifyEmail.errors.resendCta',
  },
  email_already_verified: {
    i18nKey: 'auth.verifyEmail.errors.alreadyVerified',
    type: 'toast',
  },
  email_taken: {
    i18nKey: 'auth.verifyEmail.errors.taken',
    type: 'inline',
  },

  // account deletion lifecycle.
  // The i18n keys live under
  // account.deletion.errors.* in messages/{ru,en}.json. Do not change
  // the entry shape — fixed by §4.
  sole_owner_of_businesses: {
    // Triggers the SoleOwnerBlockedModal via the delete-confirm
    // submit handler. The 'modal' type is the canonical render hint
    // for fallback toast surfaces.
    i18nKey: 'account.deletion.errors.soleOwner',
    type: 'modal',
  },
  account_pending_deletion: {
    // 423 from the grace-gate middleware. Body carries deletionDate
    // for the toast interpolation; the persistent banner mounted in
    // (app)/layout.tsx separately shows the date from /auth/me.
    i18nKey: 'account.deletion.errors.pendingDeletion',
    type: 'banner',
    toastTone: 'destructive',
    actionLabel: 'account.deletion.errors.pendingDeletionAction',
    actionHref: '/settings/account',
  },
  account_deleted: {
    i18nKey: 'account.deletion.errors.accountDeleted',
    type: 'toast',
  },
  password_invalid: {
    // 401 from DELETE /users/me with wrong password. Inline field
    // error under the password input on the delete-confirm modal.
    i18nKey: 'account.deletion.errors.passwordInvalid',
    type: 'inline',
  },
  deletion_too_old: {
    // 410 from POST /users/me/restore past the 30d grace window.
    // Toast + redirect to /login.
    i18nKey: 'account.deletion.errors.tooOld',
    type: 'toast',
    toastTone: 'destructive',
  },
  restore_window_expired: {
    // Alias for deletion_too_old surfaced via a different code on
    // some legacy paths — kept for backward-compat.
    i18nKey: 'account.deletion.errors.tooOld',
    type: 'toast',
    toastTone: 'destructive',
  },
  no_deletion_pending: {
    // 404 from POST /users/me/restore when no pending deletion.
    // Usually only reached via stale UI — toast + reload.
    i18nKey: 'account.deletion.errors.noPending',
    type: 'toast',
  },
  origin_not_allowed: {
    // 403 from POST /users/me/restore CSRF guard. Should not happen
    // from the dashboard; toast for visibility.
    i18nKey: 'account.deletion.errors.originNotAllowed',
    type: 'toast',
    toastTone: 'destructive',
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
