import { describe, it, expect } from 'vitest';

import { chatErrorKey, CHAT_ERROR_FALLBACK_KEY } from '../chatError';

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

  it('returns the generic fallback key for an unknown code', () => {
    expect(chatErrorKey('never_emitted_code')).toBe(CHAT_ERROR_FALLBACK_KEY);
  });

  it('returns the generic fallback key for a missing code', () => {
    expect(chatErrorKey(undefined)).toBe(CHAT_ERROR_FALLBACK_KEY);
  });
});
