// Maps a resolve HTTP error to the exact Russian Sonner toast string per
// UI-SPEC §Error toasts (17-UI-SPEC.md) + Plan 17-09 closure of
// VERIFICATION item 6 (403 → dedicated copy). Called ONLY for resolve
// failures; resume-stream errors use the separate RESUME_STREAM_ERROR
// constant.
//
// Callers (currently `useChat.resolveApproval` in hooks/useChat.ts) must
// pass the HTTP status code and the parsed JSON body (or null when the body
// could not be parsed). A caught `fetch` exception (network failure, DNS
// error, connection refused) is treated as the generic connection branch.
//
// Branch precedence (top wins):
//   1. status === 409          → "уже была обработана"      (Phase 16 D-03 race)
//   2. status === 403          → "вне вашей бизнес-области" (Plan 16-07 scope auth)
//   3. body.reason === policy_revoked → "запрещён политикой" (Phase 16 D-12 TOCTOU)
//   4. default                 → "Ошибка соединения"        (network / 5xx / unknown)

export const RESUME_STREAM_ERROR = 'Ошибка продолжения — перезагрузите страницу';

export function resolveErrorToRussian(status: number, body: unknown): string {
  // 409 → concurrent resolve lost the race (Phase 16 D-03).
  if (status === 409) return 'Ошибка: операция уже была обработана';

  // 403 → resolve handler rejected the request because the requester's
  // business scope does not match `batch.business_id` (Plan 16-07 auth
  // check). Distinct from 409 (race) and policy_revoked (TOCTOU). The
  // generic connection-error copy was misleading operators into retrying
  // a permission failure — see 17-VERIFICATION.md item 6. Wins over a
  // body.reason='policy_revoked' precedence: a 403 is auth/scope, NOT a
  // policy gate, even if the server happened to attach a policy_revoked
  // body shape.
  if (status === 403) return 'Отказано: операция вне вашей бизнес-области';

  // Policy revocation can surface on any 4xx per Phase 16 D-12.
  const reason = (body as { reason?: unknown } | null | undefined)?.reason;
  if (reason === 'policy_revoked') return 'Отказано: инструмент запрещён текущей политикой';

  // Fall-through: 400-with-editable, other 4xx, 5xx, network-thrown → generic.
  return 'Ошибка соединения — попробуйте ещё раз';
}
