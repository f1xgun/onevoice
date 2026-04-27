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

  it('maps body.reason === "policy_revoked" on a non-403 status → policy-revoked toast', () => {
    // Plan 17-09 changes the 403 branch to win over body-shape parsing —
    // see the dedicated 17-09 block below. policy_revoked still wins on
    // any other 4xx (e.g. 400) per Phase 16 D-12.
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

  // ---------- Plan 17-09 / VERIFICATION item 6: dedicated 403 toast ----------
  // The resolve handler returns 403 when the requester's business scope does
  // not match `batch.business_id` (Plan 16-07 auth check). Previously this
  // fell through to the generic connection error, misleading operators into
  // retrying a permission failure. The new branch returns scope-accurate
  // copy distinct from 409 (race), policy_revoked (TOCTOU), and the
  // generic network/5xx fallback.

  it('JJ) maps HTTP 403 with null body → "Отказано: операция вне вашей бизнес-области"', () => {
    expect(resolveErrorToRussian(403, null)).toBe('Отказано: операция вне вашей бизнес-области');
  });

  it('KK) maps HTTP 403 with arbitrary body shape → 403 dedicated toast', () => {
    expect(resolveErrorToRussian(403, { reason: 'forbidden' })).toBe(
      'Отказано: операция вне вашей бизнес-области'
    );
    expect(resolveErrorToRussian(403, undefined)).toBe(
      'Отказано: операция вне вашей бизнес-области'
    );
    expect(resolveErrorToRussian(403, { error: 'business scope mismatch' })).toBe(
      'Отказано: операция вне вашей бизнес-области'
    );
  });

  it('LL) 403 wins over policy_revoked body precedence (auth/scope > policy)', () => {
    // Per VERIFICATION.md item 6, a 403 is an auth/scope failure NOT a
    // policy revocation. If the server returns 403 with a body that also
    // says reason=policy_revoked, the 403 branch wins because the user is
    // crossing a trust boundary, not just hitting a policy gate.
    expect(resolveErrorToRussian(403, { reason: 'policy_revoked' })).toBe(
      'Отказано: операция вне вашей бизнес-области'
    );
  });
});
