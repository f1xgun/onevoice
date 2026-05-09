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
