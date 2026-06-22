// Pure code→i18n-key mapper for stream-level chat error frames. Mirrors the
// tasks-page `explainError` mechanism: the orchestrator stamps a stable code
// on the `error` SSE frame (step.go / resume.go) and the frontend resolves it
// to a localized message under `chat.streamError.*`. The raw Go error string
// is never surfaced as the headline — unknown codes fall through to a generic
// localized fallback so a non-technical operator never sees `[Error: ...]`.
//
// Lives in its own module so the mapping can be unit-tested without pulling in
// the MessageBubble client tree (next-intl + React).

import type { ChatErrorCode } from '@/types/chat';

// Generic fallback i18n key (under chat.streamError) used for unknown or
// missing codes. Exported so callers and tests share one source of truth.
export const CHAT_ERROR_FALLBACK_KEY = 'fallback';

// Closed map of known stream-error codes → their i18n key under
// `chat.streamError`. Codes mirror the orchestrator's Event.Code values.
const CHAT_ERROR_KEYS: Record<ChatErrorCode, string> = {
  max_iterations: 'maxIterations',
  internal_error: 'internalError',
  conversation_token_cap: 'conversationTokenCap',
  daily_spend_exceeded: 'dailySpendExceeded',
  rate_limit_unavailable: 'rateLimitUnavailable',
  rate_limit_exceeded: 'rateLimitExceeded',
  approval_expired: 'approvalExpired',
};

// chatErrorKey resolves a stream-error code to its i18n key under
// `chat.streamError`. Undefined / unknown codes return the generic fallback.
export function chatErrorKey(code: string | undefined): string {
  if (code && code in CHAT_ERROR_KEYS) {
    return CHAT_ERROR_KEYS[code as ChatErrorCode];
  }
  return CHAT_ERROR_FALLBACK_KEY;
}
