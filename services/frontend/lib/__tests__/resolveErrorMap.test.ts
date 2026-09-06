import { describe, it, expect, vi } from 'vitest';
import type { AxiosError } from 'axios';
import { createTranslator } from 'next-intl';
import ruMessages from '@/messages/ru.json';
import enMessages from '@/messages/en.json';
import {
  createMapEmailVerificationError,
  createMapInviteError,
  createMapMemberError,
  createMapRoleError,
  createMapTelegramConnectError,
  createResolveErrorMap,
  isEmailVerificationRequiredError,
} from '../resolveErrorMap';

// We feed the request-scoped factories with the real `ru` bundle via
// `createTranslator` (the same primitive the production helper uses).
// The behavior under test is the BRANCH selection inside each
// factory — the localized copy is asserted exactly to pin the contract
// every consumer (toasts, refusal cards) currently relies on.
function tFor(namespace: string) {
  return createTranslator({ locale: 'ru', messages: ruMessages, namespace });
}

const { resolveError: resolveErrorToRussian, resumeStreamError: RESUME_STREAM_ERROR } =
  createResolveErrorMap(tFor('common.errors') as (key: string) => string);

const mapMemberError = createMapMemberError(tFor('team.errors') as (key: string) => string);
const mapInviteError = createMapInviteError(
  tFor('invite.accept.errors') as (key: string) => string,
  tFor('team.invite.errors') as (key: string) => string
);
const mapRoleError = createMapRoleError(tFor('roles.errors') as (key: string) => string);

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

  it('exposes resumeStreamError value with the exact UI-SPEC string', () => {
    expect(RESUME_STREAM_ERROR).toBe('Ошибка продолжения — перезагрузите страницу');
  });

  it('JJ) maps HTTP 403 with null body → "Отказано: это действие выходит за рамки вашей организации"', () => {
    expect(resolveErrorToRussian(403, null)).toBe(
      'Отказано: это действие выходит за рамки вашей организации'
    );
  });

  it('KK) maps HTTP 403 with arbitrary body shape → 403 dedicated toast', () => {
    expect(resolveErrorToRussian(403, { reason: 'forbidden' })).toBe(
      'Отказано: это действие выходит за рамки вашей организации'
    );
    expect(resolveErrorToRussian(403, undefined)).toBe(
      'Отказано: это действие выходит за рамки вашей организации'
    );
    expect(resolveErrorToRussian(403, { error: 'business scope mismatch' })).toBe(
      'Отказано: это действие выходит за рамки вашей организации'
    );
  });

  it('LL) 403 wins over policy_revoked body precedence (auth/scope > policy)', () => {
    expect(resolveErrorToRussian(403, { reason: 'policy_revoked' })).toBe(
      'Отказано: это действие выходит за рамки вашей организации'
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

describe('mapRoleError', () => {
  it('maps 403 cannot_grant_unowned_permissions → roles.errors.cannotGrantUnowned', () => {
    expect(mapRoleError(axiosErr(403, { error: 'cannot_grant_unowned_permissions' }))).toBe(
      'Нельзя выдать права, которых у вас нет. Уберите серые галочки.'
    );
  });

  it('maps 422 self_lockout → roles.errors.selfLockout', () => {
    expect(mapRoleError(axiosErr(422, { error: 'self_lockout' }))).toBe(
      'Нельзя забрать у себя право редактировать роли — иначе потеряете доступ к этой странице.'
    );
  });

  it('maps 422 system_role_immutable → roles.errors.systemRoleImmutable', () => {
    expect(mapRoleError(axiosErr(422, { error: 'system_role_immutable' }))).toBe(
      'Системные роли нельзя изменять.'
    );
  });

  it('maps 422 role_in_use → roles.errors.roleInUse', () => {
    expect(mapRoleError(axiosErr(422, { error: 'role_in_use' }))).toBe(
      'На эту роль назначены участники. Выберите, на какую роль их перенести.'
    );
  });

  it('maps 422 last_owner → roles.errors.lastOwnerOnDelete', () => {
    expect(mapRoleError(axiosErr(422, { error: 'last_owner' }))).toBe(
      'Нельзя удалить роль — иначе в организации не останется владельцев.'
    );
  });

  it('falls back to roles.errors.saveGeneric for unrecognised codes', () => {
    expect(mapRoleError(axiosErr(500, { error: 'internal_server_error' }))).toBe(
      'Не удалось сохранить роль. Попробуйте ещё раз.'
    );
  });

  it('handles undefined / plain Error / network errors without throwing', () => {
    expect(mapRoleError(undefined)).toBe('Не удалось сохранить роль. Попробуйте ещё раз.');
    expect(mapRoleError(new Error('network down'))).toBe(
      'Не удалось сохранить роль. Попробуйте ещё раз.'
    );
  });
});

describe('email verification gate (412)', () => {
  const mapVerify = createMapEmailVerificationError(
    tFor('auth.verifyEmail.errors') as (key: string) => string
  );

  // The gate body shape is { code, verifiedDeadline } — `code`, NOT the
  // `error` field the shared axiosErr() helper builds.
  function gateErr(status: number, data?: Record<string, unknown>): AxiosError {
    return {
      isAxiosError: true,
      response: { status, data: data ?? {}, statusText: '', headers: {}, config: {} as never },
    } as unknown as AxiosError;
  }

  it('detects a 412 { code: email_verification_required }', () => {
    expect(
      isEmailVerificationRequiredError(
        gateErr(412, {
          code: 'email_verification_required',
          verifiedDeadline: '2026-05-24T18:29:47Z',
        })
      )
    ).toBe(true);
  });

  it('ignores a 412 without the verification code', () => {
    expect(isEmailVerificationRequiredError(gateErr(412, { code: 'something_else' }))).toBe(false);
    expect(isEmailVerificationRequiredError(gateErr(412, {}))).toBe(false);
  });

  it('ignores the verification code on a non-412 status', () => {
    expect(
      isEmailVerificationRequiredError(gateErr(403, { code: 'email_verification_required' }))
    ).toBe(false);
  });

  it('ignores network / non-axios throws without throwing', () => {
    expect(isEmailVerificationRequiredError(undefined)).toBe(false);
    expect(isEmailVerificationRequiredError(new Error('network'))).toBe(false);
  });

  it('maps the gate to the canonical localized message, else null', () => {
    expect(mapVerify(gateErr(412, { code: 'email_verification_required' }))).toBe(
      'Подтвердите email, чтобы продолжить.'
    );
    expect(mapVerify(gateErr(500, { error: 'internal' }))).toBeNull();
    expect(mapVerify(undefined)).toBeNull();
  });
});

const { createTranslator: createRealTranslator } = await vi.importActual<{
  createTranslator: typeof createTranslator;
}>('next-intl');

describe.each([
  {
    locale: 'ru',
    messages: ruMessages,
    expected:
      'Этот канал уже подключён к другой организации. Отключите его от неё или выберите другой канал.',
  },
  {
    locale: 'en',
    messages: enMessages,
    expected:
      'This channel is already connected to another organization. Disconnect it from that organization or choose another channel.',
  },
])('Telegram connect errors ($locale)', ({ locale, messages, expected }) => {
  const t = createRealTranslator({ locale, messages, namespace: 'integrations.telegramErrors' });
  const mapError = createMapTelegramConnectError(t as (key: string) => string);

  it.each([
    { error: 'Этот канал уже подключён к другой организации' },
    { error: 'This channel is already connected to another organization' },
    { reason: 'already_connected' },
    {},
  ])('maps a connect conflict with body %j to actionable localized copy', (body) => {
    const message = mapError(axiosErr(409, body));
    expect(message).toBe(expected);
    expect(message).not.toBe(t('failed'));
  });

  it.each(['not_admin', 'no_post_rights'])(
    'preserves the explicit %s reason on a conflict',
    (reason) => {
      expect(mapError(axiosErr(409, { reason }))).toBe(t('adminRequired'));
    }
  );

  it('does not interpret other status codes as an existing connection', () => {
    expect(mapError(axiosErr(400, { error: 'unknown error' }))).toBe(t('failed'));
  });
});
