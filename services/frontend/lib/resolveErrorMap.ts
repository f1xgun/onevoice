import type { AxiosError } from 'axios';
import { useMemo } from 'react';
import { useTranslations } from 'next-intl';

import { HTTP_STATUS } from '@/lib/constants/httpStatus';

// Request-scoped error-map factories. Each factory takes a namespaced
// translator and returns a closure; React consumers use the matching
// `useXxxError` hook, which memoizes on translator identity. The closure
// is safe to pass to event handlers and React Query mutation callbacks.
//
// Strings live under:
// common.errors.*               → resolve/resume flow
// team.errors.*                 → /members mutations
// invite.accept.errors.*        → invitation accept/preview
// team.invite.errors.*          → invite-form 429
// roles.errors.*                → /roles mutations
//
// Tests pass a translator stub returning the key — they assert which
// branch picks which key, not the localized copy.

type TranslateFn = (key: string) => string;

// ----- resolve / resume -----------------------------------------------------

/**
 * Maps a resolve HTTP error to a localized Sonner toast string. Called
 * ONLY for resolve failures; resume-stream errors use the separate
 * `resumeStreamError(...)` value from the same factory.
 *
 * Callers must pass the HTTP status code and the parsed JSON body (or
 * null when the body could not be parsed). A caught `fetch` exception
 * (network failure, DNS error, connection refused) is treated as the
 * generic connection branch.
 *
 * Branch precedence (top wins):
 *   1. status === 409          → "уже была обработана"      (race)
 *   2. status === 403          → "вне вашей бизнес-области" (scope auth)
 *   3. body.reason === policy_revoked → "запрещён политикой" (TOCTOU)
 *   4. default                 → "Ошибка соединения"        (network / 5xx / unknown)
 */
export interface ResolveErrorMap {
  resolveError: (status: number, body: unknown) => string;
  resumeStreamError: string;
}

export function createResolveErrorMap(tErrors: TranslateFn): ResolveErrorMap {
  return {
    resolveError(status: number, body: unknown): string {
      if (status === HTTP_STATUS.CONFLICT) return tErrors('alreadyHandled');
      if (status === HTTP_STATUS.FORBIDDEN) return tErrors('outOfScope');
      const reason = (body as { reason?: unknown } | null | undefined)?.reason;
      if (reason === 'policy_revoked') return tErrors('policyRevoked');
      return tErrors('connectionRetry');
    },
    resumeStreamError: tErrors('resumeStream'),
  };
}

export function useResolveErrorMap(): ResolveErrorMap {
  const t = useTranslations('common.errors') as TranslateFn;
  return useMemo(() => createResolveErrorMap(t), [t]);
}

// ----- shared status/code extraction ---------------------------------------

interface ApiErrorBody {
  error?: string;
  reason?: string;
}

function extractStatusAndCode(err: unknown): {
  status: number | undefined;
  code: string | undefined;
  reason: string | undefined;
} {
  const axiosErr = err as AxiosError<ApiErrorBody> | undefined;
  const status = axiosErr?.response?.status;
  const code = axiosErr?.response?.data?.error;
  const reason = axiosErr?.response?.data?.reason;
  return { status, code, reason };
}

// Pulls the backend error code from an axios-shaped error
// (`response.data.error`). Returns `undefined` when the error has no such
// field (network failure, non-axios throw). Callers typically `?? ''` or
// `|| fallback` the result for their toast copy.
export function extractApiErrorCode(err: unknown): string | undefined {
  return extractStatusAndCode(err).code;
}

// ----- email-verification gate (cross-cutting 412) -------------------------

const EMAIL_VERIFICATION_REQUIRED_CODE = 'email_verification_required';

// The soft-restrict middleware (RequireVerifiedEmailDay0/Day7) returns 412
// with a body shaped `{ code, verifiedDeadline }` — note the `code` field,
// NOT the `error` field every other endpoint uses. extractApiErrorCode reads
// `.error`, so it deliberately does not match this gate; detect it here.
export function isEmailVerificationRequiredError(err: unknown): boolean {
  const axiosErr = err as AxiosError<{ code?: string }> | undefined;
  return (
    axiosErr?.response?.status === HTTP_STATUS.PRECONDITION_FAILED &&
    axiosErr.response.data?.code === EMAIL_VERIFICATION_REQUIRED_CODE
  );
}

// Maps the 412 verification gate to its canonical localized toast string,
// or null when `err` is something else so callers fall through to their own
// context-specific failure copy. Mirrors the useMapXError mapper idiom; the
// string lives under auth.verifyEmail.errors.required.
export function createMapEmailVerificationError(
  tVerifyErrors: TranslateFn
): (err: unknown) => string | null {
  return (err) => (isEmailVerificationRequiredError(err) ? tVerifyErrors('required') : null);
}

export function useMapEmailVerificationError(): (err: unknown) => string | null {
  const t = useTranslations('auth.verifyEmail.errors') as TranslateFn;
  return useMemo(() => createMapEmailVerificationError(t), [t]);
}

// ----- central error-code registry -----------------------------------------

// A registry entry pairs an HTTP status + optional error code with the
// translation key to render. `code === undefined` matches the status alone
// (used for blanket 410 / 500 fallbacks where the body code is ignored).
// `fallback: true` marks the catch-all the lookup falls through to when
// no specific (status, code) match wins.
type ErrorMapEntry = {
  status?: number;
  code?: string;
  messageKey: string;
  fallback?: true;
};

// Per-context entry lists. Order matters only for `fallback` selection;
// specific (status, code) matches are tried first regardless of position.
// Codes shared across contexts (`last_owner`, `self_lockout`) live here so
// each consumer's translation key stays local without duplicating the
// status/code knowledge.
const ERROR_CODES_BY_CONTEXT = {
  members: [
    { status: 422, code: 'last_owner', messageKey: 'lastOwner' },
    { status: 422, code: 'self_lockout', messageKey: 'selfLockout' },
    { status: 422, code: 'system_role_immutable', messageKey: 'systemRoleImmutable' },
    { messageKey: 'generic', fallback: true },
  ],
  invites: [
    { status: 410, messageKey: 'gone.body' },
    { status: 409, code: 'already_member', messageKey: 'alreadyMember.body' },
    // 429 too_many_pending lives under team.invite.errors, not invite.accept;
    // the lookup uses an alt-namespace marker so the mapper picks the right
    // translator.
    { status: 429, code: 'too_many_pending', messageKey: 'altNs:tooManyPending' },
    { messageKey: 'generic', fallback: true },
  ],
  roles: [
    {
      status: 403,
      code: 'cannot_grant_unowned_permissions',
      messageKey: 'cannotGrantUnowned',
    },
    { status: 422, code: 'self_lockout', messageKey: 'selfLockout' },
    { status: 422, code: 'system_role_immutable', messageKey: 'systemRoleImmutable' },
    { status: 422, code: 'role_in_use', messageKey: 'roleInUse' },
    { status: 422, code: 'last_owner', messageKey: 'lastOwnerOnDelete' },
    { messageKey: 'saveGeneric', fallback: true },
  ],
} satisfies Record<string, ReadonlyArray<ErrorMapEntry>>;

type ErrorContext = keyof typeof ERROR_CODES_BY_CONTEXT;

// resolveErrorKey picks the first specific (status, code) entry that
// matches, then a status-only entry, then the fallback. Returns
// { messageKey } so the calling mapper can route to its translator.
function resolveErrorKey(context: ErrorContext, err: unknown): string {
  const { status, code } = extractStatusAndCode(err);
  const entries = ERROR_CODES_BY_CONTEXT[context];
  let fallbackKey = '';
  for (const entry of entries) {
    if (entry.fallback) {
      fallbackKey = entry.messageKey;
      continue;
    }
    if (entry.status !== status) continue;
    if (entry.code !== undefined && entry.code !== code) continue;
    return entry.messageKey;
  }
  return fallbackKey;
}

// ----- members --------------------------------------------------------------

/**
 * Maps backend errors from `/businesses/{id}/members/...` mutations to a
 * localized toast string. Mappings live in ERROR_CODES_BY_CONTEXT.members.
 * Callers should pass the returned string to `toast.error(...)`.
 */
export function createMapMemberError(tTeamErrors: TranslateFn): (err: unknown) => string {
  return (err) => tTeamErrors(resolveErrorKey('members', err));
}

export function useMapMemberError(): (err: unknown) => string {
  const t = useTranslations('team.errors') as TranslateFn;
  return useMemo(() => createMapMemberError(t), [t]);
}

// ----- invitations ----------------------------------------------------------

/**
 * Maps backend errors from invitation flows (create, revoke, preview,
 * accept) to a localized string. Mappings live in
 * ERROR_CODES_BY_CONTEXT.invites. The `altNs:` prefix on a messageKey
 * routes through the secondary translator (team.invite.errors) instead of
 * the default (invite.accept.errors) — that lets the single registry
 * cover both namespaces without splitting the context.
 */
export function createMapInviteError(
  tInviteAcceptErrors: TranslateFn,
  tInviteFormErrors: TranslateFn
): (err: unknown) => string {
  return (err) => {
    const key = resolveErrorKey('invites', err);
    if (key.startsWith('altNs:')) return tInviteFormErrors(key.slice('altNs:'.length));
    return tInviteAcceptErrors(key);
  };
}

export function useMapInviteError(): (err: unknown) => string {
  const tAccept = useTranslations('invite.accept.errors') as TranslateFn;
  const tForm = useTranslations('team.invite.errors') as TranslateFn;
  return useMemo(() => createMapInviteError(tAccept, tForm), [tAccept, tForm]);
}

// ----- roles ----------------------------------------------------------------

/**
 * Maps backend errors from `/businesses/{id}/roles/...` mutations to a
 * localized toast string. Mappings live in ERROR_CODES_BY_CONTEXT.roles.
 * DeleteRoleDialog intercepts `role_in_use` separately (race-recovery
 * in-place swap) BEFORE calling the closure.
 */
export function createMapRoleError(tRolesErrors: TranslateFn): (err: unknown) => string {
  return (err) => tRolesErrors(resolveErrorKey('roles', err));
}

export function useMapRoleError(): (err: unknown) => string {
  const t = useTranslations('roles.errors') as TranslateFn;
  return useMemo(() => createMapRoleError(t), [t]);
}

export function isBusinessPlanLimitError(err: unknown): boolean {
  const data = (err as AxiosError<{ code?: string; error?: string }> | undefined)?.response?.data;
  return data?.code === 'plan_limit_businesses' || data?.error === 'plan_limit_businesses';
}

export function createMapTelegramConnectError(t: TranslateFn): (err: unknown) => string {
  const reasonKeys: Record<string, string> = {
    unreachable: 'unreachable',
    rate_limited: 'rateLimited',
    forbidden: 'noAccess',
    api_rejected: 'channelNotFound',
    not_admin: 'adminRequired',
    no_post_rights: 'adminRequired',
    already_connected: 'alreadyConnected',
  };
  return (err) => {
    const response = (err as AxiosError<{ reason?: string }> | undefined)?.response;
    const reason = response?.data?.reason;
    if (reason) return t(Object.hasOwn(reasonKeys, reason) ? reasonKeys[reason] : 'failed');
    if (response?.status === HTTP_STATUS.CONFLICT) return t('alreadyConnected');
    if (!response || response.status >= HTTP_STATUS.INTERNAL_SERVER_ERROR) return t('unreachable');
    if (response.status === HTTP_STATUS.TOO_MANY_REQUESTS) return t('rateLimited');
    return t('failed');
  };
}
