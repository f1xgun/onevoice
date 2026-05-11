import { describe, it, expect } from 'vitest';
import type { AxiosError } from 'axios';
import {
  resolveErrorToRussian,
  RESUME_STREAM_ERROR,
  mapInviteError,
  mapMemberError,
} from '../resolveErrorMap';

function axiosErr(status: number, body?: { error?: string; reason?: string }): AxiosError {
  return {
    isAxiosError: true,
    response: { status, data: body ?? {}, statusText: '', headers: {}, config: {} as never },
  } as unknown as AxiosError;
}

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
    // The 403 branch wins over body-shape parsing —
    // see the dedicated block below. policy_revoked still wins on
    // any other 4xx (e.g. 400).
    expect(resolveErrorToRussian(400, { reason: 'policy_revoked', detail: 'tool denied' })).toBe(
      'Отказано: инструмент запрещён текущей политикой'
    );
  });

  it('maps 400 with an editable list to the generic connection toast', () => {
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

  // ---------- dedicated 403 toast ----------
  // The resolve handler returns 403 when the requester's business scope does
  // not match `batch.business_id` (auth check). Previously this
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
    // A 403 is an auth/scope failure NOT a
    // policy revocation. If the server returns 403 with a body that also
    // says reason=policy_revoked, the 403 branch wins because the user is
    // crossing a trust boundary, not just hitting a policy gate.
    expect(resolveErrorToRussian(403, { reason: 'policy_revoked' })).toBe(
      'Отказано: операция вне вашей бизнес-области'
    );
  });
});

describe('mapMemberError', () => {
  it('maps 422 last_owner → team.errors.lastOwner', () => {
    expect(mapMemberError(axiosErr(422, { error: 'last_owner' }))).toBe(
      'Нельзя удалить последнего владельца. Сначала назначьте нового владельца.'
    );
  });

  it('maps 422 self_lockout → team.errors.selfLockout', () => {
    expect(mapMemberError(axiosErr(422, { error: 'self_lockout' }))).toBe(
      'Нельзя забрать у себя право управлять ролями.'
    );
  });

  it('maps 422 system_role_immutable → team.errors.systemRoleImmutable', () => {
    expect(mapMemberError(axiosErr(422, { error: 'system_role_immutable' }))).toBe(
      'Системные роли нельзя изменять.'
    );
  });

  it('maps 422 with unrecognised code → team.errors.generic', () => {
    expect(mapMemberError(axiosErr(422, { error: 'some_other_code' }))).toBe(
      'Не удалось выполнить действие.'
    );
  });

  it('maps 500 → team.errors.generic', () => {
    expect(mapMemberError(axiosErr(500))).toBe('Не удалось выполнить действие.');
  });

  it('handles undefined / plain Error / network errors without throwing', () => {
    expect(mapMemberError(undefined)).toBe('Не удалось выполнить действие.');
    expect(mapMemberError(new Error('network down'))).toBe('Не удалось выполнить действие.');
    expect(mapMemberError({ isAxiosError: true } as AxiosError)).toBe(
      'Не удалось выполнить действие.'
    );
  });
});

describe('mapInviteError', () => {
  it('maps 410 (any body) → invite.accept.errors.gone.body', () => {
    expect(mapInviteError(axiosErr(410))).toBe(
      'Возможно, срок ссылки истёк или её отозвали. Попросите ссылку заново.'
    );
    expect(mapInviteError(axiosErr(410, { error: 'expired' }))).toBe(
      'Возможно, срок ссылки истёк или её отозвали. Попросите ссылку заново.'
    );
  });

  it('maps 409 already_member → invite.accept.errors.alreadyMember.body', () => {
    expect(mapInviteError(axiosErr(409, { error: 'already_member' }))).toBe(
      'Откройте панель и выберите её в переключателе организаций сверху.'
    );
  });

  it('maps 429 too_many_pending → team.invite.errors.tooManyPending', () => {
    expect(mapInviteError(axiosErr(429, { error: 'too_many_pending' }))).toBe(
      'Достигнут лимит — 20 ожидающих приглашений на организацию. Отзовите старые, чтобы создать новое.'
    );
  });

  it('maps 409 with unrecognised code → invite.accept.errors.generic (no alreadyMember collision)', () => {
    expect(mapInviteError(axiosErr(409, { error: 'some_other_conflict' }))).toBe(
      'Не удалось обработать приглашение. Попробуйте ещё раз.'
    );
  });

  it('maps 500 → invite.accept.errors.generic', () => {
    expect(mapInviteError(axiosErr(500))).toBe(
      'Не удалось обработать приглашение. Попробуйте ещё раз.'
    );
  });

  it('handles undefined / plain Error / network errors without throwing', () => {
    expect(mapInviteError(undefined)).toBe(
      'Не удалось обработать приглашение. Попробуйте ещё раз.'
    );
    expect(mapInviteError(new Error('network down'))).toBe(
      'Не удалось обработать приглашение. Попробуйте ещё раз.'
    );
  });
});
