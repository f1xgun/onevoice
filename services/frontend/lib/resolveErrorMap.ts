// Maps a resolve HTTP error to the exact Russian Sonner toast string per
// UI-SPEC §Error toasts (17-UI-SPEC.md). Called ONLY for resolve failures;
// resume-stream errors use the separate RESUME_STREAM_ERROR constant.
//
// Callers (currently `useChat.resolveApproval` in hooks/useChat.ts) must
// pass the HTTP status code and the parsed JSON body (or null when the body
// could not be parsed). A caught `fetch` exception (network failure, DNS
// error, connection refused) is treated as the generic connection branch.

export const RESUME_STREAM_ERROR = 'Ошибка продолжения — перезагрузите страницу';

export function resolveErrorToRussian(status: number, body: unknown): string {
  // 409 → concurrent resolve lost the race (Phase 16 D-03).
  if (status === 409) return 'Ошибка: операция уже была обработана';

  // Policy revocation can surface on any 4xx per Phase 16 D-12.
  const reason = (body as { reason?: unknown } | null | undefined)?.reason;
  if (reason === 'policy_revoked') return 'Отказано: инструмент запрещён текущей политикой';

  // Fall-through: 400-with-editable, other 4xx, 5xx, network-thrown → generic.
  return 'Ошибка соединения — попробуйте ещё раз';
}
