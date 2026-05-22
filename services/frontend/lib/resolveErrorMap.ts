import type { AxiosError } from 'axios';
import { useMemo } from 'react';
import { useTranslations } from 'next-intl';

import { HTTP_STATUS } from '@/lib/constants/httpStatus';

// Request-scoped error-map factories (Phase B1).
//
// Each factory takes a namespaced translator and returns a closure over
// that translator. React consumers use the matching `useXxxError()` hook,
// which calls `useTranslations(...)` and memoizes the closure on the
// translator's identity. The closure is safe to pass to event handlers
// and React Query mutation callbacks.
//
// Strings live under:
//   common.errors.*               → resolve/resume flow
//   team.errors.*                 → /members mutations
//   invite.accept.errors.*        → invitation accept/preview
//   team.invite.errors.*          → invite-form 429
//   roles.errors.*                → /roles mutations
//
// Tests pass a translator stub that returns the key — they assert mapping
// BEHAVIOR (which key the branch picks), not the localized copy.

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
      // 409 → concurrent resolve lost the race.
      if (status === HTTP_STATUS.CONFLICT) return tErrors('alreadyHandled');
      // 403 → resolve handler rejected the request because the requester's
      // business scope does not match `batch.business_id` (auth check).
      // Distinct from 409 (race) and policy_revoked (TOCTOU). Wins over a
      // body.reason='policy_revoked' precedence: a 403 is auth/scope, NOT
      // a policy gate, even if the server happened to attach a
      // policy_revoked body shape.
      if (status === HTTP_STATUS.FORBIDDEN) return tErrors('outOfScope');
      // Policy revocation can surface on any 4xx.
      const reason = (body as { reason?: unknown } | null | undefined)?.reason;
      if (reason === 'policy_revoked') return tErrors('policyRevoked');
      // Fall-through: 400-with-editable, other 4xx, 5xx, network-thrown → generic.
      return tErrors('connectionRetry');
    },
    resumeStreamError: tErrors('resumeStream'),
  };
}

export function useResolveErrorMap(): ResolveErrorMap {
  const t = useTranslations('common.errors') as TranslateFn;
  return useMemo(() => createResolveErrorMap(t), [t]);
}

// ----- members --------------------------------------------------------------

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

/**
 * Maps backend errors from `/businesses/{id}/members/...` mutations to a
 * localized toast string. Recognised codes:
 *   - 422 last_owner             → team.errors.lastOwner
 *   - 422 self_lockout           → team.errors.selfLockout
 *   - 422 system_role_immutable  → team.errors.systemRoleImmutable
 *   - anything else              → team.errors.generic
 *
 * Callers should pass the returned string to `toast.error(...)`.
 */
export function createMapMemberError(tTeamErrors: TranslateFn): (err: unknown) => string {
  return (err) => {
    const { status, code } = extractStatusAndCode(err);
    if (status === HTTP_STATUS.UNPROCESSABLE_ENTITY && code === 'last_owner')
      return tTeamErrors('lastOwner');
    if (status === HTTP_STATUS.UNPROCESSABLE_ENTITY && code === 'self_lockout')
      return tTeamErrors('selfLockout');
    if (status === HTTP_STATUS.UNPROCESSABLE_ENTITY && code === 'system_role_immutable')
      return tTeamErrors('systemRoleImmutable');
    return tTeamErrors('generic');
  };
}

export function useMapMemberError(): (err: unknown) => string {
  const t = useTranslations('team.errors') as TranslateFn;
  return useMemo(() => createMapMemberError(t), [t]);
}

// ----- invitations ----------------------------------------------------------

/**
 * Maps backend errors from invitation flows (create, revoke, preview,
 * accept) to a localized string. The caller (accept page, invite modal)
 * decides whether to render the result as a full-card refusal (for 410
 * / 409) or as a toast (for other failures).
 *
 * Mappings:
 *   - 410 (any reason)        → invite.accept.errors.gone.body
 *   - 409 already_member      → invite.accept.errors.alreadyMember.body
 *   - 429 too_many_pending    → team.invite.errors.tooManyPending
 *   - anything else           → invite.accept.errors.generic
 */
export function createMapInviteError(
  tInviteAcceptErrors: TranslateFn,
  tInviteFormErrors: TranslateFn
): (err: unknown) => string {
  return (err) => {
    const { status, code } = extractStatusAndCode(err);
    if (status === HTTP_STATUS.GONE) return tInviteAcceptErrors('gone.body');
    if (status === HTTP_STATUS.CONFLICT && code === 'already_member')
      return tInviteAcceptErrors('alreadyMember.body');
    if (status === HTTP_STATUS.TOO_MANY_REQUESTS && code === 'too_many_pending')
      return tInviteFormErrors('tooManyPending');
    return tInviteAcceptErrors('generic');
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
 * localized toast string. Recognised codes (Plan 05-03):
 *   - 403 cannot_grant_unowned_permissions → roles.errors.cannotGrantUnowned
 *   - 422 self_lockout                     → roles.errors.selfLockout
 *   - 422 system_role_immutable            → roles.errors.systemRoleImmutable
 *   - 422 role_in_use                      → roles.errors.roleInUse
 *   - 422 last_owner                       → roles.errors.lastOwnerOnDelete
 *   - anything else                        → roles.errors.saveGeneric
 *
 * Callers surface the result via `toast.error(...)`. DeleteRoleDialog
 * intercepts `role_in_use` separately (race-recovery in-place swap,
 * CONTEXT D-10) BEFORE calling the closure — see Plan 05-06.
 */
export function createMapRoleError(tRolesErrors: TranslateFn): (err: unknown) => string {
  return (err) => {
    const { status, code } = extractStatusAndCode(err);
    if (status === HTTP_STATUS.FORBIDDEN && code === 'cannot_grant_unowned_permissions')
      return tRolesErrors('cannotGrantUnowned');
    if (status === HTTP_STATUS.UNPROCESSABLE_ENTITY && code === 'self_lockout')
      return tRolesErrors('selfLockout');
    if (status === HTTP_STATUS.UNPROCESSABLE_ENTITY && code === 'system_role_immutable')
      return tRolesErrors('systemRoleImmutable');
    if (status === HTTP_STATUS.UNPROCESSABLE_ENTITY && code === 'role_in_use')
      return tRolesErrors('roleInUse');
    if (status === HTTP_STATUS.UNPROCESSABLE_ENTITY && code === 'last_owner')
      return tRolesErrors('lastOwnerOnDelete');
    return tRolesErrors('saveGeneric');
  };
}

export function useMapRoleError(): (err: unknown) => string {
  const t = useTranslations('roles.errors') as TranslateFn;
  return useMemo(() => createMapRoleError(t), [t]);
}
