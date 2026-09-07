import { act, render, waitFor, screen, cleanup } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { describe, expect, it, vi } from 'vitest';
import AppLayout from '@/app/(app)/layout';
import SettingsLayout from '@/app/(app)/settings/layout';
import SettingsPage from '@/app/(app)/settings/page';
import ChatListPage from '@/app/(app)/chat/page';
import PublicLayout from '@/app/(public)/layout';
import LoginPage from '@/app/(public)/login/page';
import type * as ConvHooks from '@/hooks/useConversations';
import type * as ProjHooks from '@/hooks/useProjects';
import { hasLayoutBrowser, withLayoutPage } from '@/test-utils/browser-layout';
import { SkipLink } from '@/components/a11y/skip-link';
import { mkdirSync } from 'node:fs';
import BillingPage from '@/app/(app)/settings/billing/page';
import IntegrationsPage from '@/app/(app)/integrations/page';
import { ProfileForm } from '@/components/business/ProfileForm';
import { TelegramConnectModal } from '@/components/integrations/TelegramConnectModal';

const usePathnameMock = vi.fn(() => '/chat');
vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: vi.fn(), back: vi.fn(), replace: vi.fn() }),
  usePathname: () => usePathnameMock(),
  useSearchParams: () => new URLSearchParams(),
}));

vi.mock('@/lib/auth', async () => {
  const tokenStore = {
    user: null as unknown,
    accessToken: 'test-token' as string | null,
    isAuthenticated: true,
  };
  const setters = {
    setAuth: (user: unknown, token: string) => {
      tokenStore.user = user;
      tokenStore.accessToken = token;
      tokenStore.isAuthenticated = true;
    },
    setAccessToken: (token: string) => {
      tokenStore.accessToken = token;
      tokenStore.isAuthenticated = !!token;
    },
    logout: () => {
      tokenStore.user = null;
      tokenStore.accessToken = null;
      tokenStore.isAuthenticated = false;
    },
  };
  const useAuthStore = Object.assign(
    (selector?: (s: unknown) => unknown) => {
      const state = { ...tokenStore, ...setters };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ ...tokenStore, ...setters }) }
  );
  return { useAuthStore };
});

vi.mock('@/lib/api', () => ({
  api: {
    get: vi.fn(() => Promise.resolve({ data: [] })),
    post: vi.fn(() => Promise.resolve({ data: { accessToken: 'test-token' } })),
    put: vi.fn(),
    delete: vi.fn(),
  },
}));

vi.mock('@/lib/telemetry', () => ({
  trackEvent: vi.fn(),
  trackClick: vi.fn(),
}));
vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (selector?: (s: unknown) => unknown) => {
    const state = {
      activeBusinessId: 'test-biz-id',
      setActive: vi.fn(),
      clear: vi.fn(),
    };
    return selector ? selector(state) : state;
  },
}));

vi.mock('@/lib/hooks/useBusinessList', () => ({
  useBusinessList: () => ({
    data: [
      {
        id: 'test-biz-id',
        name: 'Test',
        role: { id: 'r1', name: 'owner' },
        status: 'active',
        joined_at: '2024-01-01',
      },
    ],
    isLoading: false,
  }),
  BUSINESS_LIST_QUERY_KEY: ['businesses'],
}));

vi.mock('@/lib/queryClient', () => ({
  queryClient: { invalidateQueries: vi.fn() },
}));

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock('@/hooks/useConversations', async () => {
  const actual = await vi.importActual<typeof ConvHooks>('@/hooks/useConversations');
  return {
    ...actual,
    useConversationsQuery: () => ({ data: [], isLoading: false, error: null }),
  };
});
vi.mock('@/hooks/useProjects', async () => {
  const actual = await vi.importActual<typeof ProjHooks>('@/hooks/useProjects');
  return {
    ...actual,
    useProjectsQuery: () => ({ data: [], isLoading: false, error: null }),
  };
});

function Wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

vi.mock('@/lib/hooks/usePermission', () => ({
  usePermission: () => ({ allowed: true, isLoading: false }),
}));

const fixture = vi.hoisted(() => ({ state: 'success' }));
vi.mock('@/lib/hooks/usePlatforms', () => ({
  usePlatforms: () => ({
    platforms: [
      { id: 'telegram', fullLabel: 'Telegram', status: 'active' },
      { id: 'google_business', fullLabel: 'Google Business Profile', status: 'active' },
    ],
  }),
}));
vi.mock('@/lib/api/business-api', () => ({
  bizApi: () => ({
    get: async (path: string) => {
      if (fixture.state === 'error') throw new Error('offline');
      if (fixture.state === 'loading') return new Promise(() => {});
      if (fixture.state === 'malformed' && path === '/integrations') return { data: {} };
      if (path === '/integrations')
        return {
          data:
            fixture.state === 'empty'
              ? []
              : [
                  {
                    id: 'channel-1',
                    platform: 'telegram',
                    status: 'active',
                    externalId: '@test_account_one',
                    metadata: { title: 'Example organization — first account' },
                  },
                  {
                    id: 'channel-2',
                    platform: 'telegram',
                    status: 'token_expired',
                    externalId: '@test_account_two',
                    metadata: { title: 'Example organization — second account' },
                  },
                ],
        };
      return { data: { id: 'test-biz-id', name: 'Example organization' } };
    },
    post: vi.fn(),
  }),
}));
vi.mock('@/components/integrations/IntegrationsSyncPanel', () => ({
  IntegrationsSyncPanel: () => null,
}));
vi.mock('@/app/(app)/settings/billing/_lib/useBillingSummary', () => ({
  useBillingSummary: () => ({
    data: {
      plan: { code: 'pro', name: 'Pro', monthly_credits: 2000 },
      credits: { granted: 2000, used: 500, remaining: 1500, overage: 0 },
      usage_this_month: { actions: 42, spend_usd: 12.34, images: 7 },
      daily_spend: { today_usd: 1.5, cap_usd: 5 },
    },
    isLoading: false,
    isSuccess: true,
    error: null,
  }),
}));

const cases = [
  'login',
  'profile',
  'settings',
  'integrations',
  'billing',
  'connection',
  'error',
  'empty',
] as const;

describe('working surface browser matrix', () => {
  it.skipIf(!hasLayoutBrowser).each(cases)(
    'captures the actual %s composition in both themes, locales and widths',
    async (surface) => {
      mkdirSync('artifacts/forms', { recursive: true });
      for (const locale of ['ru', 'en'] as const) {
        (
          globalThis as unknown as { __setTestLocale: (locale: 'ru' | 'en') => void }
        ).__setTestLocale(locale);
        fixture.state = surface;
        const component =
          surface === 'login' ? (
            <PublicLayout>
              <LoginPage />
            </PublicLayout>
          ) : surface === 'profile' ? (
            <main id="main-content" className="mx-auto max-w-3xl p-4">
              <ProfileForm defaultValues={{ name: 'Example organization', category: 'cafe' }} />
            </main>
          ) : surface === 'settings' ? (
            <SettingsPage />
          ) : surface === 'billing' ? (
            <BillingPage />
          ) : surface === 'connection' ? (
            <TelegramConnectModal open onClose={() => {}} />
          ) : (
            <IntegrationsPage />
          );
        const rendered = render(
          <Wrapper>
            <SkipLink />
            {component}
          </Wrapper>
        );
        await act(async () => {
          await new Promise((resolve) => setTimeout(resolve, 100));
        });
        if (surface === 'integrations')
          expect(screen.getByText('@test_account_two')).toBeInTheDocument();
        if (surface === 'billing') expect(screen.getByText('$12.34')).toBeInTheDocument();
        for (const theme of ['light', 'dark']) {
          for (const width of [375, 1440]) {
            await withLayoutPage(document.body.innerHTML, { width, height: 900 }, async (page) => {
              await page.locator('html').evaluate((element, theme) => {
                element.setAttribute('class', theme);
              }, theme);
              expect(
                await page.evaluate(() => {
                  document.documentElement.style.scrollBehavior = 'auto';
                  window.scrollTo(100, 0);
                  return window.scrollX;
                })
              ).toBe(0);
              await page.screenshot({
                path: `artifacts/forms/${surface}-${locale}-${theme}-${width}.png`,
                fullPage: true,
              });
            });
          }
        }
        rendered.unmount();
      }
    },
    60000
  );
});

it.each(['error', 'malformed'])(
  'renders a recoverable integration list error for %s responses without disconnected accounts',
  async (state) => {
    fixture.state = state;
    render(
      <Wrapper>
        <IntegrationsPage />
      </Wrapper>
    );
    expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось');
    expect(screen.queryByText('Не подключено')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Повторить' })).toBeVisible();
  }
);

it('does not show empty accounts while loading, then shows successful empty data', async () => {
  fixture.state = 'loading';
  render(
    <Wrapper>
      <IntegrationsPage />
    </Wrapper>
  );
  expect(screen.queryByText('Не подключено')).not.toBeInTheDocument();
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  cleanup();
  fixture.state = 'empty';
  render(
    <Wrapper>
      <IntegrationsPage />
    </Wrapper>
  );
  await waitFor(() => expect(screen.getAllByText('Не подключено')).toHaveLength(2));
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
});
