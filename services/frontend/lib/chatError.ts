// Pure code→i18n-key mapper for stream-level chat error frames. Mirrors the
// tasks-page `explainError` mechanism: the orchestrator stamps a stable code
// on the `error` SSE frame (step.go / resume.go) and the frontend resolves it
// to a localized message under `chat.streamError.*`. The raw Go error string
// is never surfaced as the headline — unknown codes fall through to a generic
// localized fallback so a non-technical operator never sees `[Error: ...]`.
//
// Lives in its own module so the mapping can be unit-tested without pulling in
// the MessageBubble client tree (next-intl + React).

import { HTTP_STATUS } from '@/lib/constants/httpStatus';
import type { ChatErrorCode } from '@/types/chat';

// Generic fallback i18n key (under chat.streamError) used for unknown or
// missing codes. Exported so callers and tests share one source of truth.
export const CHAT_ERROR_FALLBACK_KEY = 'fallback';

// Closed map of known stream-error codes → their i18n key under
// `chat.streamError`. Codes mirror the orchestrator's Event.Code values plus
// the pre-stream codes the chat POST returns before the SSE body opens
// (sse_concurrency_exceeded, business_not_found, orchestrator_unavailable).
const CHAT_ERROR_KEYS: Record<ChatErrorCode, string> = {
  stream_interrupted: 'streamInterrupted',
  max_iterations: 'maxIterations',
  internal_error: 'internalError',
  conversation_token_cap: 'conversationTokenCap',
  daily_spend_exceeded: 'dailySpendExceeded',
  rate_limit_unavailable: 'rateLimitUnavailable',
  rate_limit_exceeded: 'rateLimitExceeded',
  approval_expired: 'approvalExpired',
  sse_concurrency_exceeded: 'sseConcurrencyExceeded',
  business_not_found: 'businessNotFound',
  orchestrator_unavailable: 'orchestratorUnavailable',
};

// chatErrorKey resolves a stream-error code to its i18n key under
// `chat.streamError`. Undefined / unknown codes return the generic fallback.
export function chatErrorKey(code: string | undefined): string {
  if (code && code in CHAT_ERROR_KEYS) {
    return CHAT_ERROR_KEYS[code as ChatErrorCode];
  }
  return CHAT_ERROR_FALLBACK_KEY;
}

// Maps an HTTP status returned by the chat POST (before the SSE body opens) to
// its canonical pre-stream chat-error code, when the body carried no typed
// `code` of its own. The chat POST returns a machine `code` only on 429
// (sse_concurrency_exceeded) and 503 (rate_limit_unavailable); the 404 / 502 /
// 400 outcomes carry a plain `{ error }` shape with no code, so they are
// derived from the status here. Returns undefined when the status has no known
// mapping, so callers fall through to the generic localized fallback.
function preStreamCodeForStatus(status: number): ChatErrorCode | undefined {
  switch (status) {
    case HTTP_STATUS.NOT_FOUND:
      return 'business_not_found';
    case HTTP_STATUS.BAD_GATEWAY:
      return 'orchestrator_unavailable';
    case HTTP_STATUS.SERVICE_UNAVAILABLE:
      return 'rate_limit_unavailable';
    default:
      return undefined;
  }
}

export interface PreStreamChatError {
  code?: ChatErrorCode;
  detail?: string;
  retryAfterSeconds?: number;
}

interface PreStreamErrorBody {
  code?: string;
  error?: string;
  retry_after_s?: number;
}

// mapPreStreamChatError translates a non-2xx chat-POST response (HTTP status +
// parsed JSON body) into the `error` SSE shape the message renderer already
// understands: a machine-readable `code` (localized via chatErrorKey →
// chat.streamError.*) plus an optional raw `detail` for the diagnostics
// affordance. The body's own typed `code` wins (429/503); otherwise the code
// is derived from the status. `retry_after_s` is surfaced so the caller can
// hint a retry window. Unknown statuses leave `code` undefined → generic
// fallback (never the raw connectionError collapse).
export function mapPreStreamChatError(status: number, body: unknown): PreStreamChatError {
  const parsed = (body ?? null) as PreStreamErrorBody | null;
  const bodyCode = parsed?.code;
  const code =
    bodyCode && bodyCode in CHAT_ERROR_KEYS
      ? (bodyCode as ChatErrorCode)
      : preStreamCodeForStatus(status);
  const detail = typeof parsed?.error === 'string' ? parsed.error : undefined;
  const retryAfterSeconds =
    typeof parsed?.retry_after_s === 'number' ? parsed.retry_after_s : undefined;
  return { code, detail, retryAfterSeconds };
}
