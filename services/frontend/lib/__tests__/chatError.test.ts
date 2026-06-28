import { describe, it, expect } from 'vitest';

import { chatErrorKey, mapPreStreamChatError, CHAT_ERROR_FALLBACK_KEY } from '../chatError';

describe('chatErrorKey', () => {
  it('maps known stream-error codes to their i18n key', () => {
    expect(chatErrorKey('max_iterations')).toBe('maxIterations');
    expect(chatErrorKey('internal_error')).toBe('internalError');
    expect(chatErrorKey('conversation_token_cap')).toBe('conversationTokenCap');
    expect(chatErrorKey('daily_spend_exceeded')).toBe('dailySpendExceeded');
    expect(chatErrorKey('rate_limit_unavailable')).toBe('rateLimitUnavailable');
    expect(chatErrorKey('rate_limit_exceeded')).toBe('rateLimitExceeded');
    expect(chatErrorKey('approval_expired')).toBe('approvalExpired');
  });

  it('maps the pre-stream chat-POST codes to their i18n key', () => {
    expect(chatErrorKey('sse_concurrency_exceeded')).toBe('sseConcurrencyExceeded');
    expect(chatErrorKey('business_not_found')).toBe('businessNotFound');
    expect(chatErrorKey('orchestrator_unavailable')).toBe('orchestratorUnavailable');
  });

  it('returns the generic fallback key for an unknown code', () => {
    expect(chatErrorKey('never_emitted_code')).toBe(CHAT_ERROR_FALLBACK_KEY);
  });

  it('returns the generic fallback key for a missing code', () => {
    expect(chatErrorKey(undefined)).toBe(CHAT_ERROR_FALLBACK_KEY);
  });
});

describe('mapPreStreamChatError', () => {
  it('prefers the body code on a 429 concurrency error and surfaces retry_after_s', () => {
    expect(
      mapPreStreamChatError(429, { code: 'sse_concurrency_exceeded', retry_after_s: 1 })
    ).toEqual({
      code: 'sse_concurrency_exceeded',
      detail: undefined,
      retryAfterSeconds: 1,
    });
  });

  it('prefers the body code on a 503 rate-limit error', () => {
    expect(mapPreStreamChatError(503, { code: 'rate_limit_unavailable' })).toMatchObject({
      code: 'rate_limit_unavailable',
    });
  });

  it('derives the code from the status when the body carries only { error }', () => {
    expect(mapPreStreamChatError(404, { error: 'business not found' })).toEqual({
      code: 'business_not_found',
      detail: 'business not found',
      retryAfterSeconds: undefined,
    });
    expect(mapPreStreamChatError(502, { error: 'orchestrator unavailable' })).toMatchObject({
      code: 'orchestrator_unavailable',
      detail: 'orchestrator unavailable',
    });
  });

  it('leaves the code undefined (generic fallback) but keeps the detail for an unmapped status', () => {
    expect(mapPreStreamChatError(400, { error: 'message is required' })).toEqual({
      code: undefined,
      detail: 'message is required',
      retryAfterSeconds: undefined,
    });
  });

  it('ignores an unknown body code and falls back to the status mapping', () => {
    expect(mapPreStreamChatError(503, { code: 'totally_unknown' })).toMatchObject({
      code: 'rate_limit_unavailable',
    });
  });

  it('tolerates a null body', () => {
    expect(mapPreStreamChatError(404, null)).toEqual({
      code: 'business_not_found',
      detail: undefined,
      retryAfterSeconds: undefined,
    });
  });
});
