import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { createTranslator } from 'next-intl';
import SettingsLayout from '../layout';

vi.mock('next-intl/server', () => ({
  getTranslations: async (namespace: string) =>
    createTranslator({ locale: 'ru', messages: {}, namespace }),
}));
vi.mock('@/lib/hooks/usePermission', () => ({
  usePermission: () => ({ allowed: false, isLoading: false }),
}));

describe('settings navigation', () => {
  it.each([
    ['ru', 'Аккаунт'],
    ['en', 'Account'],
  ] as const)(
    'exposes the account page in %s without organization permissions',
    async (locale, label) => {
      (globalThis as unknown as { __setTestLocale: (locale: string) => void }).__setTestLocale(
        locale
      );
      render(await SettingsLayout({ children: <p>Settings content</p> }));
      expect(screen.getByRole('link', { name: label })).toHaveAttribute(
        'href',
        '/settings/account'
      );
    }
  );
});
