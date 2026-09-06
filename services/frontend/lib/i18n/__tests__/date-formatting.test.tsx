import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { createFormatter } from 'next-intl';
import { IntlClientProvider } from '@/components/IntlClientProvider';
import { EffectiveTag } from '@/components/legal/EffectiveTag';
import { WithdrawalPanel } from '@/components/legal/WithdrawalPanel';
import requestConfig from '../request';

vi.unmock('next-intl');
vi.mock('next/headers', () => ({
  cookies: () => ({ get: () => ({ value: 'en' }) }),
  headers: () => ({ get: () => null }),
}));
vi.mock('next-intl/server', () => ({ getRequestConfig: (resolve: unknown) => resolve }));
vi.mock('@/lib/api/consents', () => ({
  listMyConsents: async () => [
    { slug: 'tos', version: 'v1.0', acceptedAt: '2026-09-05T22:08:00Z' },
  ],
  withdrawPDN: vi.fn(),
}));

describe('request and client date formatting', () => {
  it.each(['ru', 'en'] as const)(
    'uses Moscow dates and acceptance wording in %s',
    async (locale) => {
      const config = await requestConfig({ requestLocale: Promise.resolve(locale) });
      const messages = (await import(`../../../messages/${locale}.json`)).default;
      const onError = vi.spyOn(console, 'error').mockImplementation(() => {});
      const format = createFormatter({ ...config, locale, messages });
      expect(config.timeZone).toBe('Europe/Moscow');
      const expected = format.dateTime(new Date('2026-09-05T22:08:00Z'), {
        day: 'numeric',
        month: 'long',
        year: 'numeric',
      });
      expect(expected).toContain('6');
      render(
        <IntlClientProvider locale={locale} messages={messages}>
          <EffectiveTag effectiveFrom="2026-09-05T22:08:00Z" version="v1.0" />
          <WithdrawalPanel />
        </IntlClientProvider>
      );
      expect(
        await screen.findByText(
          `${locale === 'ru' ? 'Принято' : 'Accepted'} ${expected} · ${locale === 'ru' ? 'версия' : 'version'} v1.0`
        )
      ).toBeInTheDocument();
      expect(
        screen.getByText(
          `${locale === 'ru' ? 'Действует с' : 'Effective from'} ${expected} · ${locale === 'ru' ? 'версия' : 'version'} v1.0`
        )
      ).toBeInTheDocument();
      expect(onError).not.toHaveBeenCalled();
      onError.mockRestore();
    }
  );
});
