import { describe, it, expect } from 'vitest';

import { explainError } from '../explainError';
import type { AgentTask } from '@/types/task';

function task(overrides: Partial<AgentTask>): AgentTask {
  return {
    id: 't1',
    businessId: 'b1',
    type: 'send_channel_post',
    status: 'error',
    platform: 'telegram',
    createdAt: '2026-05-31T00:00:00Z',
    ...overrides,
  };
}

describe('explainError', () => {
  it('integration_token_invalid + telegram returns tokenTelegram + reconnectTelegram CTA', () => {
    const out = explainError(
      task({ errorCode: 'integration_token_invalid', platform: 'telegram' })
    );
    expect(out.summaryKey).toBe('tokenTelegram');
    expect(out.cta?.labelKey).toBe('reconnectTelegram');
    expect(out.cta?.href).toBe('/integrations?reconnect=telegram');
  });

  it('integration_token_invalid + vk returns tokenVk + reconnectVk CTA', () => {
    const out = explainError(task({ errorCode: 'integration_token_invalid', platform: 'vk' }));
    expect(out.summaryKey).toBe('tokenVk');
    expect(out.cta?.labelKey).toBe('reconnectVk');
    expect(out.cta?.href).toBe('/integrations?reconnect=vk');
  });

  it('integration_token_invalid + yandex_business returns tokenGeneric + reconnectYandex CTA', () => {
    const out = explainError(
      task({ errorCode: 'integration_token_invalid', platform: 'yandex_business' })
    );
    expect(out.summaryKey).toBe('tokenGeneric');
    expect(out.cta?.labelKey).toBe('reconnectYandex');
    expect(out.cta?.href).toBe('/integrations?reconnect=yandex_business');
  });

  it('integration_token_invalid + google_business returns tokenGeneric + reconnectGoogle CTA', () => {
    const out = explainError(
      task({ errorCode: 'integration_token_invalid', platform: 'google_business' })
    );
    expect(out.summaryKey).toBe('tokenGeneric');
    expect(out.cta?.labelKey).toBe('reconnectGoogle');
    expect(out.cta?.href).toBe('/integrations?reconnect=google_business');
  });

  it('rate_limit_exceeded returns rateLimit + autoRetryHint', () => {
    const out = explainError(task({ errorCode: 'rate_limit_exceeded' }));
    expect(out.summaryKey).toBe('rateLimit');
    expect(out.willAutoRetry).toBe(true);
    expect(out.cta).toBeUndefined();
  });

  it('transient returns transient + autoRetryHint', () => {
    const out = explainError(task({ errorCode: 'transient' }));
    expect(out.summaryKey).toBe('transient');
    expect(out.willAutoRetry).toBe(true);
  });

  it('channel_not_found returns notFound + openIntegrations CTA', () => {
    const out = explainError(task({ errorCode: 'channel_not_found' }));
    expect(out.summaryKey).toBe('notFound');
    expect(out.cta?.labelKey).toBe('openIntegrations');
    expect(out.cta?.href).toBe('/integrations');
  });

  it('media_too_large returns media (no CTA, no retry hint)', () => {
    const out = explainError(task({ errorCode: 'media_too_large' }));
    expect(out.summaryKey).toBe('media');
    expect(out.cta).toBeUndefined();
    expect(out.willAutoRetry).toBeFalsy();
  });

  it('undefined errorCode (historical row) returns fallback', () => {
    const out = explainError(task({ errorCode: undefined }));
    expect(out.summaryKey).toBe('fallback');
    expect(out.willAutoRetry).toBe(true);
  });

  it('unknown code (forward-compat) returns fallback', () => {
    const out = explainError(task({ errorCode: 'never_emitted_code' as never }));
    expect(out.summaryKey).toBe('fallback');
  });
});
