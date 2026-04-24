import { describe, it, expect } from 'vitest';
import { resolveErrorToRussian, RESUME_STREAM_ERROR } from '../resolveErrorMap';

describe('resolveErrorToRussian', () => {
  it('maps HTTP 409 → "operation already processed" toast', () => {
    expect(resolveErrorToRussian(409, { error: 'batch resolving', retry_after_ms: 500 })).toBe(
      'Ошибка: операция уже была обработана'
    );
  });

  it('maps 409 with arbitrary body shape to the same 409 string', () => {
    expect(resolveErrorToRussian(409, null)).toBe('Ошибка: операция уже была обработана');
    expect(resolveErrorToRussian(409, 'unexpected string body')).toBe(
      'Ошибка: операция уже была обработана'
    );
  });

  it('maps any status with body.reason === "policy_revoked" → policy-revoked toast', () => {
    expect(resolveErrorToRussian(403, { reason: 'policy_revoked' })).toBe(
      'Отказано: инструмент запрещён текущей политикой'
    );
    // Backend may return policy_revoked inside a 400 payload as well.
    expect(resolveErrorToRussian(400, { reason: 'policy_revoked', detail: 'tool denied' })).toBe(
      'Отказано: инструмент запрещён текущей политикой'
    );
  });

  it('maps 400 with an editable list (D-12) to the generic connection toast', () => {
    expect(
      resolveErrorToRussian(400, {
        error: 'field X not editable for tool Y',
        editable: ['text', 'parse_mode'],
      })
    ).toBe('Ошибка соединения — попробуйте ещё раз');
  });

  it('maps 500 → generic connection toast', () => {
    expect(resolveErrorToRussian(500, { error: 'internal' })).toBe(
      'Ошибка соединения — попробуйте ещё раз'
    );
  });

  it('maps network-thrown style (no status, null body) → generic connection toast', () => {
    expect(resolveErrorToRussian(0, null)).toBe('Ошибка соединения — попробуйте ещё раз');
  });

  it('maps an unexpected 418 without policy_revoked → generic connection toast', () => {
    expect(resolveErrorToRussian(418, { reason: 'i-am-a-teapot' })).toBe(
      'Ошибка соединения — попробуйте ещё раз'
    );
  });

  it('handles missing reason key gracefully', () => {
    expect(resolveErrorToRussian(400, { error: 'malformed' })).toBe(
      'Ошибка соединения — попробуйте ещё раз'
    );
  });

  it('handles null body gracefully (no TypeError)', () => {
    expect(resolveErrorToRussian(500, null)).toBe('Ошибка соединения — попробуйте ещё раз');
  });

  it('handles undefined body gracefully', () => {
    expect(resolveErrorToRussian(500, undefined)).toBe('Ошибка соединения — попробуйте ещё раз');
  });

  it('exposes RESUME_STREAM_ERROR constant with the exact UI-SPEC string', () => {
    expect(RESUME_STREAM_ERROR).toBe('Ошибка продолжения — перезагрузите страницу');
  });
});
