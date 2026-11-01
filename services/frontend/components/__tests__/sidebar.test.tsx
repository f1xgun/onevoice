import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { Sidebar } from '../sidebar';
import { hasLayoutBrowser, withLayoutPage } from '@/test-utils/browser-layout';
import type { Project } from '@/types/project';

// Mock next/navigation — usePathname drives the projects-subtree visibility gate.
const pathnameRef: { current: string } = { current: '/chat' };
vi.mock('next/navigation', () => ({
  usePathname: () => pathnameRef.current,
  useRouter: () => ({ push: vi.fn(), back: vi.fn(), replace: vi.fn() }),
}));

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock('@/lib/api', () => ({
  api: {
    get: vi.fn(() => Promise.resolve({ data: [] })),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  },
}));

vi.mock('@/lib/auth', () => ({
  useAuthStore: (
    selector: (state: { user: { email: string } | null; logout: () => void }) => unknown
  ) => selector({ user: { email: 'tester@example.com' }, logout: vi.fn() }),
}));

const sampleProject: Project = {
  id: 'p-1',
  businessId: 'b-1',
  name: 'Отзывы',
  description: '',
  systemPrompt: '',
  whitelistMode: 'inherit',
  allowedTools: [],
  quickActions: [],
  createdAt: '2026-04-18T00:00:00Z',
  updatedAt: '2026-04-18T00:00:00Z',
};

vi.mock('@/hooks/useProjects', () => ({
  useProjectsQuery: () => ({ data: [sampleProject] }),
}));

vi.mock('@/hooks/useConversations', () => ({
  useConversationsQuery: () => ({ data: [] }),
  useCreateConversation: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useMoveConversation: () => ({ mutateAsync: vi.fn(), isPending: false }),
}));

function Providers({ children }: { children: ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

async function renderAndOpenDrawer(pathname: string) {
  pathnameRef.current = pathname;
  const result = render(
    <Providers>
      <Sidebar />
    </Providers>
  );
  const trigger = screen.getByRole('button', { name: 'Открыть боковое меню' });
  await userEvent.setup().click(trigger);
  return result;
}

describe('Sidebar — approved mobile navigation', () => {
  beforeEach(() => {
    pathnameRef.current = '/chat';
  });

  it('sizes the Close target and dismisses the drawer without a browser', async () => {
    await renderAndOpenDrawer('/chat');
    const dialog = screen.getByRole('dialog');
    const close = screen.getByRole('button', { name: 'Закрыть', exact: true });
    expect(dialog).toHaveClass('[&>button:first-child]:h-8', '[&>button:first-child]:w-8');
    expect(dialog.querySelector('button:first-child')).toBe(close);
    await userEvent.setup().click(close);
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it.skipIf(!hasLayoutBrowser)(
    'measures the Close target at a mobile viewport',
    async () => {
      await renderAndOpenDrawer('/chat');
      await withLayoutPage(
        screen.getByRole('dialog').outerHTML,
        { width: 375, height: 667 },
        async (page) => {
          const box = await page
            .getByRole('button', { name: 'Закрыть', exact: true })
            .boundingBox();
          expect(box).not.toBeNull();
          expect(box!.width).toBeGreaterThanOrEqual(24);
          expect(box!.height).toBeGreaterThanOrEqual(24);
        }
      );
    },
    15000
  );

  it('omits duplicated history on /chat (drawer open)', async () => {
    await renderAndOpenDrawer('/chat');
    expect(screen.queryByText('Без проекта')).not.toBeInTheDocument();
    expect(screen.queryByRole('link', { name: /\+ Новый проект/ })).not.toBeInTheDocument();
  });

  it.each(['/chat/id', '/projects/new', '/projects/id', '/projects/id/chats', '/integrations'])(
    'keeps the same destinations without a project pane on %s',
    async (pathname) => {
      await renderAndOpenDrawer(pathname);
      expect(screen.queryByTestId('project-pane')).not.toBeInTheDocument();
      expect(screen.queryByText('Без проекта')).not.toBeInTheDocument();
      expect(screen.getByRole('link', { name: 'Поручения' })).toHaveAttribute('href', '/chat');
      expect(screen.getByRole('link', { name: 'Ждет вашего слова' })).toHaveAttribute(
        'href',
        '/inbox'
      );
      expect(screen.getByRole('dialog')).toHaveClass('w-full', 'max-w-none');
    }
  );
});

describe.each([
  ['ru', 'Открыть боковое меню', 'Карточка точки'],
  ['en', 'Open side menu', 'Location profile'],
] as const)('mobile organization navigation in %s', (locale, menuName, organizationName) => {
  it.skipIf(!hasLayoutBrowser)(
    'keeps the menu trigger and organization link within 320 CSS pixels',
    async () => {
      (globalThis as unknown as { __setTestLocale: (locale: 'ru' | 'en') => void }).__setTestLocale(
        locale
      );
      pathnameRef.current = '/chat';
      const { container } = render(<Sidebar />, { wrapper: Providers });
      await withLayoutPage(container.innerHTML, { width: 320, height: 800 }, async (page) => {
        const trigger = page.getByRole('button', { name: menuName });
        const bounds = (await trigger.boundingBox())!;
        expect(await trigger.isVisible()).toBe(true);
        expect(bounds.x).toBeGreaterThanOrEqual(0);
        expect(bounds.x + bounds.width).toBeLessThanOrEqual(320);
        expect(bounds.height).toBeGreaterThanOrEqual(44);
      });
      await userEvent.setup().click(screen.getByRole('button', { name: menuName }));
      await withLayoutPage(
        screen.getByRole('dialog').outerHTML,
        { width: 320, height: 800 },
        async (page) => {
          const link = page.getByRole('link', { name: organizationName, exact: true });
          const bounds = (await link.boundingBox())!;
          expect(await link.isVisible()).toBe(true);
          expect(await link.getAttribute('href')).toBe('/business');
          expect(bounds.x).toBeGreaterThanOrEqual(0);
          expect(bounds.x + bounds.width).toBeLessThanOrEqual(320);
        }
      );
    },
    30_000
  );

  it('opens the named menu by keyboard and reaches the organization link', async () => {
    (globalThis as unknown as { __setTestLocale: (locale: 'ru' | 'en') => void }).__setTestLocale(
      locale
    );
    pathnameRef.current = '/chat';
    render(<Sidebar />, { wrapper: Providers });
    const user = userEvent.setup();
    const trigger = screen.getByRole('button', { name: menuName });
    await user.tab();
    trigger.focus();
    expect(trigger).toHaveFocus();
    await user.keyboard('{Enter}');
    const link = screen.getByRole('link', { name: organizationName, exact: true });
    expect(link).toHaveAttribute('href', '/business');
    for (let index = 0; index < 20 && document.activeElement !== link; index++) {
      await user.tab();
    }
    expect(link).toHaveFocus();
    await user.keyboard('{Enter}');
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    await user.click(trigger);
    await user.click(screen.getByRole('link', { name: organizationName, exact: true }));
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });
});
