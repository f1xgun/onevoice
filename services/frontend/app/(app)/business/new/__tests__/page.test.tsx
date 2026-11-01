import { mkdirSync } from 'node:fs';
import { hasLayoutBrowser, withLayoutPage } from '@/test-utils/browser-layout';
import { beforeEach, expect, it, vi } from 'vitest';
import { render, screen, waitFor, fireEvent, within, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useBusinessStore } from '@/lib/stores/business';
import PublicOnboardingPage from '@/app/(public)/onboarding/page';
import AppLayout from '@/app/(app)/layout';
import { useAuthStore } from '@/lib/auth';
import PublicLayout from '@/app/(public)/layout';
import NewBusinessPage from '../page';
import { joinWaitlist } from '@/lib/api/waitlist';
import { api } from '@/lib/api';
import { BUSINESS_LIST_QUERY_KEY } from '@/lib/hooks/useBusinessList';

vi.mock('@/lib/api/waitlist', () => ({ joinWaitlist: vi.fn().mockResolvedValue(undefined) }));
const push = vi.fn();
const bizPut = vi.fn();
const bizGet = vi.fn();
vi.mock('next/navigation', () => ({
  useRouter: () => ({ push, replace: vi.fn(), back: vi.fn() }),
  usePathname: () => '/business/new',
  useSearchParams: () => new URLSearchParams(window.location.search),
}));
vi.mock('@/lib/api', () => ({
  api: {
    post: vi.fn(),
    get: vi.fn((path: string) =>
      Promise.resolve({
        data:
          path === '/auth/me'
            ? { id: 'owner', name: 'Owner', email: 'owner@example.org', emailVerified: true }
            : [],
      })
    ),
  },
}));
vi.mock('@/lib/api/business-api', () => ({
  bizApi: () => ({ get: bizGet, put: bizPut }),
}));
vi.mock('@/lib/hooks/usePermission', () => ({
  usePermission: () => ({ allowed: true, isError: false, isLoading: false, refetch: vi.fn() }),
}));
beforeEach(() => {
  vi.mocked(api.post).mockReset();
  push.mockReset();
  window.history.replaceState(null, '', '/business/new');
  bizGet.mockReset();
  bizGet.mockImplementation((path: string) =>
    Promise.resolve({
      data:
        path === '/voice-profile'
          ? { voiceProfile: 'Не обещать скидки.\nКак подписывать ответы: Ирина' }
          : [],
    })
  );
  bizPut.mockReset();
  bizPut.mockResolvedValue({});
});

async function fillAndSubmit(cached: boolean) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  if (cached) client.setQueryData(BUSINESS_LIST_QUERY_KEY, [{ id: 'existing', name: 'Existing' }]);
  render(
    <QueryClientProvider client={client}>
      <NewBusinessPage />
    </QueryClientProvider>
  );
  const user = userEvent.setup();
  fireEvent.change(document.getElementById('name')!, { target: { value: 'New organization' } });
  await user.click(screen.getByRole('combobox'));
  await user.click(screen.getByRole('option', { name: /Кафе/ }));
  fireEvent.change(document.getElementById('address')!, { target: { value: 'My address' } });
  await user.click(screen.getByRole('button', { name: 'Создать' }));
  return user;
}

it.each([true, false])(
  'handles server Free limit independently of list cache (%s)',
  async (cached) => {
    vi.mocked(api.post).mockRejectedValue({
      response: { status: 403, data: { code: 'plan_limit_businesses' } },
    });
    const user = await fillAndSubmit(cached);
    expect(await screen.findByRole('alert')).toHaveTextContent('Не получилось создать организацию');
    expect(document.getElementById('name')).toHaveValue('New organization');
    expect(document.getElementById('address')).toHaveValue('My address');
    expect(push).not.toHaveBeenCalled();
    await user.click(screen.getByRole('button', { name: 'Оставить заявку' }));
    const dialog = within(screen.getByRole('dialog'));
    fireEvent.change(dialog.getByRole('textbox'), { target: { value: 'owner@example.org' } });
    await user.click(dialog.getByRole('checkbox'));
    await user.click(dialog.getByRole('button', { name: 'Оставить заявку' }));
    await waitFor(() => expect(joinWaitlist).toHaveBeenCalled());
    expect(api.post).toHaveBeenCalledTimes(1);
    expect(document.getElementById('name')).toHaveValue('New organization');
  }
);

it('lets the server accept another organization without imposing a client list limit', async () => {
  vi.mocked(api.post).mockResolvedValue({ data: { id: 'new', name: 'New organization' } });
  await fillAndSubmit(true);
  expect(await screen.findByText('Где вас видят клиенты')).toBeInTheDocument();
  expect(push).not.toHaveBeenCalled();
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
});

it.skipIf(!hasLayoutBrowser)(
  'keeps launch steps inside phone and desktop widths in both themes',
  async () => {
    vi.mocked(api.post).mockResolvedValue({ data: { id: 'new', name: 'New organization' } });
    await fillAndSubmit(false);
    await screen.findByText('Где вас видят клиенты');
    mkdirSync('/private/tmp/onevoice-redesign/launch', { recursive: true });
    for (const scene of ['platforms', 'tone']) {
      if (scene === 'tone')
        await act(async () => {
          fireEvent.click(screen.getByRole('button', { name: 'Подключу позже' }));
        });
      for (const width of [375, 1280]) {
        await withLayoutPage(
          `<div class="ov-app">${document.body.innerHTML}</div>`,
          { width, height: 900 },
          async (page) => {
            for (const theme of ['light', 'dark']) {
              await page.locator('html').evaluate((html, value) => {
                html.className = value;
              }, theme);
              expect(
                await page.locator('body').evaluate((body) => body.scrollWidth)
              ).toBeLessThanOrEqual(width);
              await page.screenshot({
                path: `/private/tmp/onevoice-redesign/launch/${scene}-${width}-${theme}.png`,
                fullPage: true,
              });
            }
          }
        );
      }
    }
  },
  20000
);

it('saves the chosen tone and signature using the existing voice APIs', async () => {
  vi.mocked(api.post).mockResolvedValue({ data: { id: 'new', name: 'New organization' } });
  await fillAndSubmit(false);
  fireEvent.click(await screen.findByRole('button', { name: 'Подключу позже' }));
  expect(
    screen.getByText('Пример отзыва для настройки тона. Это не отзыв вашего клиента.')
  ).toBeVisible();
  fireEvent.change(screen.getByLabelText('Как подписывать ответы'), {
    target: { value: 'Анна, управляющая' },
  });
  fireEvent.click(screen.getByRole('button', { name: 'Готово, к делам' }));
  await waitFor(() => expect(bizPut).toHaveBeenCalledWith('/voice-tone', { tones: ['warm'] }));
  expect(bizPut).toHaveBeenCalledWith('/voice-profile', {
    voiceProfile: 'Не обещать скидки.\nКак подписывать ответы: Анна, управляющая',
  });
  await waitFor(() => expect(push).toHaveBeenCalledWith('/'));
});

it.skipIf(!hasLayoutBrowser)(
  'keeps the point form inside phone and desktop widths in both themes',
  async () => {
    render(
      <QueryClientProvider client={new QueryClient()}>
        <NewBusinessPage />
      </QueryClientProvider>
    );
    mkdirSync('/private/tmp/onevoice-redesign/launch', { recursive: true });
    for (const width of [375, 1280]) {
      await withLayoutPage(
        `<div class="ov-app">${document.body.innerHTML}</div>`,
        { width, height: 900 },
        async (page) => {
          for (const theme of ['light', 'dark']) {
            await page.locator('html').evaluate((html, value) => {
              html.className = value;
            }, theme);
            expect(
              await page.locator('body').evaluate((body) => body.scrollWidth)
            ).toBeLessThanOrEqual(width);
            await page.screenshot({
              path: `/private/tmp/onevoice-redesign/launch/point-${width}-${theme}.png`,
              fullPage: true,
            });
          }
        }
      );
    }
  },
  20000
);

it('resumes only the URL step belonging to the current business', async () => {
  useBusinessStore.getState().setActive('resume-point');
  window.history.replaceState(null, '', '/business/new?step=3&businessId=resume-point');
  render(
    <QueryClientProvider client={new QueryClient()}>
      <NewBusinessPage />
    </QueryClientProvider>
  );
  expect(await screen.findByText('Как вы отвечаете клиентам')).toBeVisible();
  expect(document.getElementById('name')).toBeNull();
});
it('creates a point on plain entry even if a business is already selected', () => {
  useBusinessStore.getState().setActive('resume-point');
  render(
    <QueryClientProvider client={new QueryClient()}>
      <NewBusinessPage />
    </QueryClientProvider>
  );
  expect(document.getElementById('name')).toBeVisible();
});
it('does not resume a URL belonging to a different business', () => {
  useBusinessStore.getState().setActive('current-point');
  window.history.replaceState(null, '', '/business/new?step=3&businessId=other-point');
  render(
    <QueryClientProvider client={new QueryClient()}>
      <NewBusinessPage />
    </QueryClientProvider>
  );
  expect(document.getElementById('name')).toBeVisible();
});
it.skipIf(!hasLayoutBrowser)(
  'checks the composed public launch route at approved gutters',
  async () => {
    window.history.replaceState(null, '', '/onboarding');
    render(
      <QueryClientProvider client={new QueryClient()}>
        <PublicLayout>
          <PublicOnboardingPage />
        </PublicLayout>
      </QueryClientProvider>
    );
    for (const width of [375, 1280]) {
      await withLayoutPage(document.body.innerHTML, { width, height: 900 }, async (page) => {
        for (const theme of ['light', 'dark']) {
          await page.locator('html').evaluate((html, value) => {
            html.className = value;
          }, theme);
          expect(
            await page.locator('body').evaluate((body) => body.scrollWidth)
          ).toBeLessThanOrEqual(width);
          await page.screenshot({
            path: `/private/tmp/onevoice-redesign/launch/public-point-${width}-${theme}.png`,
            fullPage: true,
          });
        }
      });
    }
  },
  20000
);

it.skipIf(!hasLayoutBrowser)(
  'checks the composed app launch route without nested gutters',
  async () => {
    useAuthStore
      .getState()
      .setAuth(
        { id: 'owner', name: 'Owner', email: 'owner@example.org', emailVerified: true },
        'test-token'
      );
    render(
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <AppLayout>
          <NewBusinessPage />
        </AppLayout>
      </QueryClientProvider>
    );
    await screen.findByText('Расскажите о вашей точке');
    for (const width of [375, 1280]) {
      await withLayoutPage(document.body.innerHTML, { width, height: 900 }, async (page) => {
        for (const theme of ['light', 'dark']) {
          await page.locator('html').evaluate((html, value) => {
            html.className = value;
          }, theme);
          expect(
            await page.locator('body').evaluate((body) => body.scrollWidth)
          ).toBeLessThanOrEqual(width);
          await page.screenshot({
            path: `/private/tmp/onevoice-redesign/launch/app-point-${width}-${theme}.png`,
            fullPage: true,
          });
        }
      });
    }
  },
  20000
);
