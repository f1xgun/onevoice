import type { AxiosError } from 'axios';

import { HTTP_STATUS } from '@/lib/constants/httpStatus';
import { getTranslator } from '@/lib/i18n/translator';

// Maps a resolve HTTP error to the exact Russian Sonner toast string.
// Called ONLY for resolve
// failures; resume-stream errors use the separate RESUME_STREAM_ERROR
// constant.
//
// Callers (currently `useChat.resolveApproval` in hooks/useChat.ts) must
// pass the HTTP status code and the parsed JSON body (or null when the body
// could not be parsed). A caught `fetch` exception (network failure, DNS
// error, connection refused) is treated as the generic connection branch.
//
// Branch precedence (top wins):
//   1. status === 409          → "уже была обработана"      (race)
//   2. status === 403          → "вне вашей бизнес-области" (scope auth)
//   3. body.reason === policy_revoked → "запрещён политикой" (TOCTOU)
//   4. default                 → "Ошибка соединения"        (network / 5xx / unknown)
//
// Strings live under `common.errors.*` in messages/ru.json. This module
// runs outside React (no hook context), so it pulls them via the
// module-level `getTranslator` helper.

const tErrors = getTranslator('common.errors');

export const RESUME_STREAM_ERROR = tErrors('resumeStream');

export function resolveErrorToRussian(status: number, body: unknown): string {
  // 409 → concurrent resolve lost the race.
  if (status === HTTP_STATUS.CONFLICT) return tErrors('alreadyHandled');

  // 403 → resolve handler rejected the request because the requester's
  // business scope does not match `batch.business_id` (auth
  // check). Distinct from 409 (race) and policy_revoked (TOCTOU). The
  // generic connection-error copy was misleading operators into retrying
  // a permission failure. Wins over a
  // body.reason='policy_revoked' precedence: a 403 is auth/scope, NOT a
  // policy gate, even if the server happened to attach a policy_revoked
  // body shape.
  if (status === HTTP_STATUS.FORBIDDEN) return tErrors('outOfScope');

  // Policy revocation can surface on any 4xx.
  const reason = (body as { reason?: unknown } | null | undefined)?.reason;
  if (reason === 'policy_revoked') return tErrors('policyRevoked');

  // Fall-through: 400-with-editable, other 4xx, 5xx, network-thrown → generic.
  return tErrors('connectionRetry');
}

// ---------------------------------------------------------------------------
// Phase 4 RBAC — mapMemberError + mapInviteError.
//
// These helpers convert AxiosError instances thrown by lib/api/members.ts
// and lib/api/invitations.ts into the exact Russian copy required by the
// UI-SPEC. Callers (toast handlers, refusal cards) pass the raw error
// they catch; both mappers tolerate undefined, plain Error instances, and
// network errors (no .response) without throwing.
// ---------------------------------------------------------------------------

const tTeamErrors = getTranslator('team.errors');
const tInviteAcceptErrors = getTranslator('invite.accept.errors');
const tInviteFormErrors = getTranslator('team.invite.errors');
const tRolesErrors = getTranslator('roles.errors');

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
 * Russian toast string. Recognised codes:
 *   - 422 last_owner             → team.errors.lastOwner
 *   - 422 self_lockout           → team.errors.selfLockout
 *   - 422 system_role_immutable  → team.errors.systemRoleImmutable
 *   - anything else              → team.errors.generic
 *
 * Callers should pass the returned string to `toast.error(...)`.
 */
export function mapMemberError(err: unknown): string {
  const { status, code } = extractStatusAndCode(err);
  if (status === HTTP_STATUS.UNPROCESSABLE_ENTITY && code === 'last_owner')
    return tTeamErrors('lastOwner');
  if (status === HTTP_STATUS.UNPROCESSABLE_ENTITY && code === 'self_lockout')
    return tTeamErrors('selfLockout');
  if (status === HTTP_STATUS.UNPROCESSABLE_ENTITY && code === 'system_role_immutable')
    return tTeamErrors('systemRoleImmutable');
  return tTeamErrors('generic');
}

/**
 * Maps backend errors from invitation flows (create, revoke, preview,
 * accept) to a Russian string. The caller (accept page, invite modal)
 * decides whether to render the result as a full-card refusal (for 410
 * / 409) or as a toast (for other failures) — see RESEARCH.md lines
 * 779-789.
 *
 * Mappings:
 *   - 410 (any reason)        → invite.accept.errors.gone.body
 *   - 409 already_member      → invite.accept.errors.alreadyMember.body
 *   - 429 too_many_pending    → team.invite.errors.tooManyPending
 *   - anything else           → invite.accept.errors.generic
 */
export function mapInviteError(err: unknown): string {
  const { status, code } = extractStatusAndCode(err);
  if (status === HTTP_STATUS.GONE) return tInviteAcceptErrors('gone.body');
  if (status === HTTP_STATUS.CONFLICT && code === 'already_member')
    return tInviteAcceptErrors('alreadyMember.body');
  if (status === HTTP_STATUS.TOO_MANY_REQUESTS && code === 'too_many_pending')
    return tInviteFormErrors('tooManyPending');
  return tInviteAcceptErrors('generic');
}

/**
 * Maps backend errors from `/businesses/{id}/roles/...` mutations to a Russian
 * toast string. Recognised codes (Plan 05-03):
 *   - 403 cannot_grant_unowned_permissions → roles.errors.cannotGrantUnowned
 *   - 422 self_lockout                     → roles.errors.selfLockout
 *   - 422 system_role_immutable            → roles.errors.systemRoleImmutable
 *   - 422 role_in_use                      → roles.errors.roleInUse
 *   - 422 last_owner                       → roles.errors.lastOwnerOnDelete
 *   - anything else                        → roles.errors.saveGeneric
 *
 * Callers surface the result via `toast.error(...)`. DeleteRoleDialog intercepts
 * `role_in_use` separately (race-recovery in-place swap, CONTEXT D-10) BEFORE
 * calling mapRoleError — see Plan 05-06.
 */
export function mapRoleError(err: unknown): string {
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
}
