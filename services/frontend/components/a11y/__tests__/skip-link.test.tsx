import { act, render, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { createTranslator } from 'next-intl';
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
import { SkipLink } from '../skip-link';

vi.mock('next-intl/server', () => ({
  getTranslations: async (namespace: string) =>
    createTranslator({ locale: 'ru', messages: {}, namespace }),
}));

const usePathnameMock = vi.fn(() => '/chat');
vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: vi.fn(), back: vi.fn(), replace: vi.fn() }),
  usePathname: () => usePathnameMock(),
}));

vi.mock('@/lib/auth', async () => {
  const tokenStore = { user: null as unknown, accessToken: 'test-token', isAuthenticated: true };
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

describe('skip link geometry and keyboard access in route shells', () => {
  it
    .skipIf(!hasLayoutBrowser)
    .each(
      ['/login', '/chat', '/settings'].flatMap((route) =>
        (['ru', 'en'] as const).map((locale) => ({ route, locale }))
      )
    )(
    'does not scroll horizontally and preserves keyboard navigation on $route in $locale',
    async ({ route, locale }) => {
      (globalThis as unknown as { __setTestLocale: (locale: 'ru' | 'en') => void }).__setTestLocale(
        locale
      );
      usePathnameMock.mockReturnValue(route);
      const content =
        route === '/login' ? (
          <PublicLayout>
            <LoginPage />
          </PublicLayout>
        ) : (
          <AppLayout>
            {route === '/chat' ? (
              <ChatListPage />
            ) : (
              await SettingsLayout({ children: <SettingsPage /> })
            )}
          </AppLayout>
        );
      const { container } = render(
        <Wrapper>
          <SkipLink />
          {content}
        </Wrapper>
      );
      await waitFor(() => expect(container.querySelector('main')).not.toBeNull());
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 0));
      });
      await withLayoutPage(container.innerHTML, { width: 320, height: 640 }, async (page) => {
        expect(
          await page.evaluate(() => {
            document.documentElement.style.scrollBehavior = 'auto';
            window.scrollTo(100, 0);
            return window.scrollX;
          })
        ).toBe(0);
        await page.keyboard.press('Tab');
        const link = page.locator('a[href="#main-content"]');
        expect(await link.evaluate((element) => element === document.activeElement)).toBe(true);
        const bounds = await link.boundingBox();
        expect(bounds!.x).toBeGreaterThanOrEqual(0);
        expect(bounds!.width).toBeGreaterThan(1);
        expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(320);
        expect(
          await page.evaluate(() => {
            document.documentElement.style.scrollBehavior = 'auto';
            window.scrollTo(100, 0);
            return window.scrollX;
          })
        ).toBe(0);
        await page.keyboard.press('Enter');
        expect(await page.evaluate(() => document.activeElement?.id)).toBe('main-content');
        expect(
          await page.evaluate(() => {
            document.documentElement.style.scrollBehavior = 'auto';
            window.scrollTo(100, 0);
            return window.scrollX;
          })
        ).toBe(0);
      });
    }
  );
});
