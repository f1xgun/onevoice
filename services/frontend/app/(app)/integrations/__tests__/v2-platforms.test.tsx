import { mkdirSync, readFileSync } from 'node:fs';
import { beforeEach, expect, it, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useBusinessStore } from '@/lib/stores/business';
import { hasLayoutBrowser, withLayoutPage } from '@/test-utils/browser-layout';
import IntegrationsPage from '../page';

const apiDelete = vi.hoisted(() => vi.fn().mockResolvedValue({}));
const state = vi.hoisted(() => ({
  broken: false,
  canConnect: true,
  registryError: false,
  registryLoading: false,
}));
vi.mock('next/navigation', () => ({ useSearchParams: () => new URLSearchParams() }));
vi.mock('@/components/integrations/ScopedBusinessLink', () => ({ ScopedBusinessLink: () => null }));
vi.mock('@/lib/hooks/usePermission', () => ({
  usePermission: () => ({ allowed: state.canConnect, isLoading: false }),
}));
vi.mock('@/lib/hooks/usePlatforms', () => ({
  usePlatforms: () => ({
    platforms: [
      { id: 'telegram', fullLabel: 'Telegram', status: 'active' },
      { id: 'vk', fullLabel: 'ВКонтакте', status: 'active' },
      { id: 'yandex_business', fullLabel: 'Яндекс Карты', status: 'active' },
      { id: '2gis', fullLabel: '2ГИС', status: 'coming_soon' },
      { id: 'avito', fullLabel: 'Авито', status: 'coming_soon' },
    ],
    isSuccess: !state.registryError && !state.registryLoading,
    isPending: state.registryLoading,
    isError: state.registryError,
    refetch: vi.fn(),
  }),
}));
vi.mock('@/components/integrations/IntegrationsSyncPanel', () => ({
  IntegrationsSyncPanel: () => null,
}));
vi.mock('@/lib/api/business-api', () => ({
  bizApi: () => ({
    get: async (path: string) => ({
      data:
        path === '/integrations'
          ? [
              {
                id: 'vk-1',
                platform: 'vk',
                status: state.broken ? 'token_expired' : 'active',
                externalId: '123',
                metadata: { community_name: 'Кофейня «Зёрна»' },
              },
              {
                id: 'tg-1',
                platform: 'telegram',
                status: 'active',
                externalId: '@zerna_bazhova',
                metadata: { channel_title: 'Зёрна на Бажова' },
              },
              {
                id: 'ya-1',
                platform: 'yandex_business',
                status: 'active',
                externalId: '123456',
                metadata: { yandex_user: 'Кофейня «Зёрна»' },
              },
            ]
          : path.includes('delegated-config')
            ? { available: true, rep_login: 'onevoice-zerna-47' }
            : { id: 'biz-1', name: 'Кофейня «Зёрна»' },
    }),
    post: async () => ({ data: {} }),
    delete: apiDelete,
  }),
}));

beforeEach(() => {
  Object.assign(state, {
    broken: false,
    canConnect: true,
    registryError: false,
    registryLoading: false,
  });
  apiDelete.mockClear();
  useBusinessStore.setState({ activeBusinessId: 'biz-1' });
});

function renderPage() {
  return render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <div className="ov-app">
        <IntegrationsPage />
      </div>
    </QueryClientProvider>
  );
}

it('offers real connections and keeps future platforms disabled', async () => {
  renderPage();
  expect(await screen.findByRole('heading', { name: 'Сообщество ВКонтакте' })).toBeVisible();
  const future = screen.getByRole('heading', { name: 'Скоро' }).parentElement!;
  expect(within(future).getAllByRole('button', { name: 'Скоро' })).toHaveLength(2);
  for (const button of within(future).getAllByRole('button')) expect(button).toBeDisabled();
  expect(screen.queryByRole('link', { name: 'Переподключить' })).toBeNull();
});

it('exposes reconnect only for an actor allowed to connect', async () => {
  state.broken = true;
  state.canConnect = false;
  renderPage();
  expect(await screen.findByText('Нужно переподключить')).toBeVisible();
  expect(screen.queryByRole('link', { name: 'Переподключить' })).toBeNull();
});

it('shows an explicit registry error rather than enabled fallback connections', async () => {
  state.registryError = true;
  renderPage();
  expect(await screen.findByRole('alert')).toHaveTextContent(
    'Не удалось загрузить список площадок'
  );
  expect(screen.queryByRole('button', { name: 'Подключить' })).toBeNull();
});

it('retains disconnect confirmation inside channel management', async () => {
  renderPage();
  const heading = await screen.findByRole('heading', { name: 'Сообщество ВКонтакте' });
  const summary = heading.closest('summary')!;
  await userEvent.click(summary);
  await userEvent.click(
    within(summary.closest('details')!).getByRole('button', { name: 'Отключить' })
  );
  expect(apiDelete).not.toHaveBeenCalled();
  await userEvent.click(
    within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Отключить' })
  );
  expect(apiDelete).toHaveBeenCalledWith('/integrations/vk-1');
});

it.skipIf(!hasLayoutBrowser).each(['normal', 'broken', 'telegram', 'yandex'] as const)(
  'keeps %s at phone/desktop widths in both themes',
  async (scenario) => {
    state.broken = scenario === 'broken';
    renderPage();
    await screen.findByRole('heading', { name: 'Сообщество ВКонтакте' });
    if (scenario === 'telegram' || scenario === 'yandex') {
      // A supported reconnect entry point opens the same real verification form.
      const platform = scenario === 'telegram' ? 'telegram' : 'yandex_business';
      const summary = screen
        .getByRole('heading', {
          name: scenario === 'telegram' ? 'Канал в Telegram' : 'Яндекс Карты',
        })
        .closest('summary')!;
      await userEvent.click(summary);
      await userEvent.click(
        within(summary.closest('details')!).getByRole('button', { name: '+ Добавить канал' })
      );
      if (platform === 'yandex_business') await screen.findByDisplayValue('onevoice-zerna-47');
    }
    mkdirSync('/private/tmp/onevoice-redesign/platforms', { recursive: true });
    for (const width of [375, 1280]) {
      await withLayoutPage(document.body.innerHTML, { width, height: 900 }, async (page) => {
        await page.route('**/platforms/*.svg', (route) => {
          const file = new URL(route.request().url()).pathname.split('/').pop()!;
          return route.fulfill({
            contentType: 'image/svg+xml',
            body: readFileSync(`public/platforms/${file}`, 'utf8'),
          });
        });
        // Give serialized image elements a local URL so the fixture can serve assets.
        await page.locator('img').evaluateAll((images) =>
          images.forEach((img) => {
            img.src = `http://localhost${img.getAttribute('src')}`;
          })
        );
        for (const theme of ['light', 'dark']) {
          await page.locator('html').evaluate((html, value) => {
            html.className = value;
          }, theme);
          expect(
            await page.locator('body').evaluate((body) => body.scrollWidth)
          ).toBeLessThanOrEqual(width);
          if (scenario === 'telegram' || scenario === 'yandex') {
            const box = (await page.getByRole('dialog').boundingBox())!;
            expect(box.y + box.height).toBe(900);
          }
          await page.screenshot({
            path: `/private/tmp/onevoice-redesign/platforms/${scenario}-${width}-${theme}.png`,
            fullPage: true,
          });
        }
      });
    }
  },
  20000
);
