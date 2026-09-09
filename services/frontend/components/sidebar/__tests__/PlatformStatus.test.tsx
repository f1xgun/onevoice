import { expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';

import responses from '@/test/fixtures/channel-responses.json';

import { PlatformStatus } from '../PlatformStatus';

it.each([
  ['ru', true, false, undefined, 'Проверяем подключение'],
  ['en', true, false, undefined, 'Checking connection'],
  ['ru', false, false, undefined, 'Статус пока неизвестен'],
  ['en', false, false, undefined, 'Status not yet known'],
  ['ru', false, true, responses.active.body, 'Статус пока неизвестен'],
  ['en', false, true, responses.active.body, 'Status not yet known'],
  ['ru', false, false, responses.empty.body, 'Не подключён — настройте в «Интеграциях»'],
  ['ru', false, false, responses.inconclusive.body, 'Подключено'],
] as const)(
  'renders neutral or confirmed state in %s (%s, %s)',
  (locale, pending, error, integrations, label) => {
    globalThis.__setTestLocale(locale);
    const { container } = render(
      <PlatformStatus
        label="Telegram"
        platform="telegram"
        businessId="org"
        pending={pending}
        error={error}
        integrations={integrations}
      />
    );
    expect(screen.getByText(`Telegram: ${label}`)).toBeVisible();
    expect(container.querySelector('.text-danger')).toBeNull();
  }
);

it.each([
  ['ru', 'Ошибка подключения — откройте «Интеграции»'],
  ['en', 'Connection error — open Integrations'],
] as const)('renders a confirmed error in %s', (locale, label) => {
  globalThis.__setTestLocale(locale);
  const { container } = render(
    <PlatformStatus
      label="Telegram"
      platform="telegram"
      businessId="org"
      pending={false}
      error={false}
      integrations={responses.broken.body}
    />
  );
  expect(screen.getByText(`Telegram: ${label}`)).toBeVisible();
  expect(container.querySelector('.text-danger')).toBeVisible();
});
