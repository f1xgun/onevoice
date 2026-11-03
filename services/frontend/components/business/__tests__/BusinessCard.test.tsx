import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { BusinessCard } from '../BusinessCard';
import { hasLayoutBrowser, withLayoutPage } from '@/test-utils/browser-layout';

vi.mock('@/lib/hooks/usePermission', () => ({ usePermission: () => ({ allowed: true }) }));
vi.mock('@/lib/api/business-api', () => ({
  bizApi: () => ({
    get: async () => ({
      data: [{ id: 'vk1', platform: 'vk', externalId: '55', status: 'active' }],
    }),
  }),
}));
vi.mock('@/lib/api/integrationsSync', () => ({
  fetchIntegrationsDrift: async () => [
    {
      platform: 'vk',
      externalId: '55',
      driftDetected: true,
      driftFields: ['title'],
      lastCheckedAt: '2026-11-01T07:00:00Z',
      remoteValues: { title: 'Старое название' },
      storedValues: { title: 'Кофейня «Зёрна»' },
    },
  ],
  verifyIntegrations: vi.fn(),
}));
vi.mock('@/components/integrations/DriftAlertSettings', () => ({ DriftAlertSettings: () => null }));

const business = {
  id: 'biz',
  name: 'Кофейня «Зёрна»',
  category: 'Кофейня',
  address: 'Екатеринбург, ул. Бажова, 68, вход со двора',
  phone: '+7 343 302-17-45',
  website: 'https://zerna-kofe.ru',
  description:
    'Обжариваем зерно сами, каждую неделю. Эспрессо, фильтр и альтернатива, домашняя выпечка из пекарни через дорогу.',
  settings: {
    schedule: [{ day: 'mon' as const, open: '08:00', close: '21:00', closed: false }],
    specialDates: [{ date: '2026-11-04', closed: true }],
  },
};
function fixture() {
  return render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <div className="ov-app px-gutter py-5 md:px-gutter-desktop">
        <h1 className="mb-group text-page-title">Карточка точки</h1>
        <BusinessCard business={business} businessId="biz" />
      </div>
    </QueryClientProvider>
  );
}

describe('location card', () => {
  it('shows actual differences and opens the existing schedule editor with its destination', async () => {
    fixture();
    await screen.findByText('Старое название');
    expect(screen.getByRole('button', { name: /4 ноября/ })).toBeInTheDocument();
    expect(screen.getByText('Старое название')).toHaveClass('bg-diff-old');
    expect(
      screen
        .getAllByText(business.name)
        .some((element) => element.classList.contains('bg-diff-new'))
    ).toBe(true);
    await userEvent.click(screen.getByRole('button', { name: /Понедельник/ }));
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    expect(screen.getByText(/отправим поддерживаемые поля/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Сохранить часы' })).toBeInTheDocument();
  });

  it('lists special days by date', async () => {
    render(
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <BusinessCard
          business={{
            ...business,
            settings: {
              ...business.settings,
              specialDates: [
                { date: '2026-11-08', closed: false, open: '11:00', close: '21:00' },
                { date: '2026-11-04', closed: true },
              ],
            },
          }}
          businessId="biz"
        />
      </QueryClientProvider>
    );
    const days = (await screen.findAllByRole('button', { name: /ноября/ })).map(
      (button) => button.textContent ?? ''
    );
    expect(days.findIndex((text) => text.includes('4 ноября'))).toBeLessThan(
      days.findIndex((text) => text.includes('8 ноября'))
    );
  });

  it.skipIf(!hasLayoutBrowser)(
    'fits both widths and themes',
    async () => {
      const { container } = fixture();
      await screen.findByText('Старое название');
      for (const width of [375, 1280])
        for (const theme of ['light', 'dark']) {
          await withLayoutPage(
            `<html class="${theme}"><body>${container.innerHTML}</body></html>`,
            { width, height: 1000 },
            async (page) => {
              expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width);
              await page.screenshot({
                path: `/private/tmp/onevoice-redesign/business-${width}-${theme}.png`,
                fullPage: true,
              });
            }
          );
        }
      await userEvent.click(screen.getByRole('button', { name: /Понедельник/ }));
      for (const theme of ['light', 'dark']) {
        await withLayoutPage(
          `<html class="${theme}"><body>${document.body.innerHTML}</body></html>`,
          { width: 375, height: 812 },
          async (page) => {
            expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(375);
            await page.screenshot({
              path: `/private/tmp/onevoice-redesign/business-hours-375-${theme}.png`,
            });
          }
        );
      }
    },
    30000
  );
});
