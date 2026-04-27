import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { Sidebar } from '@/components/sidebar';
import type { Project } from '@/types/project';
import type { Conversation } from '@/lib/conversations';

// Phase 19 / Plan 19-05 / D-16 — mobile drawer auto-close behavior.
//
// LOCKED CONTRACT (19-CONTEXT.md):
//   - Drawer auto-closes on chat-row select.
//   - Drawer STAYS OPEN on project-header expand/collapse.
//   - Drawer STAYS OPEN on pin/rename/delete context-menu actions.

// ----- Mocks -----

const pushMock = vi.fn();
let pathnameValue = '/chat';
vi.mock('next/navigation', () => ({
  usePathname: () => pathnameValue,
  useRouter: () => ({ push: pushMock, back: vi.fn(), replace: vi.fn() }),
}));

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() },
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

const sampleConv: Conversation = {
  id: 'c-1',
  userId: 'u-1',
  businessId: 'b-1',
  projectId: 'p-1',
  title: 'Первый чат',
  titleStatus: 'auto',
  pinned: false,
  createdAt: '2026-04-18T00:00:00Z',
  updatedAt: '2026-04-18T00:00:00Z',
};

const apiGet = vi.fn();
const apiPost = vi.fn();
const apiDelete = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    get: (...args: unknown[]) => apiGet(...args),
    post: (...args: unknown[]) => apiPost(...args),
    put: vi.fn(),
    delete: (...args: unknown[]) => apiDelete(...args),
  },
}));

function setupApi() {
  apiGet.mockImplementation((url: string) => {
    if (url === '/business') {
      return Promise.resolve({ data: { id: 'biz-1', name: 'Business' } });
    }
    if (url === '/conversations') {
      return Promise.resolve({ data: [sampleConv] });
    }
    if (url === '/projects') {
      return Promise.resolve({ data: [sampleProject] });
    }
    if (url === '/search') {
      return Promise.resolve({ data: [] });
    }
    return Promise.resolve({ data: null });
  });
}

function makeClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function Wrapper({ children }: { children: ReactNode }) {
  const client = makeClient();
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

describe('mobile drawer — Phase 19 / Plan 19-05 / D-16', () => {
  beforeEach(() => {
    apiGet.mockReset();
    apiPost.mockReset();
    apiDelete.mockReset();
    pushMock.mockReset();
    setupApi();
    pathnameValue = '/chat';
  });

  it('auto-closes when a chat row is clicked', async () => {
    const user = userEvent.setup();
    render(<Sidebar />, { wrapper: Wrapper });

    // Open the drawer.
    await user.click(screen.getByRole('button', { name: 'Открыть боковое меню' }));
    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument();
    });
    // Wait for the conversation list to hydrate.
    await waitFor(() => {
      expect(screen.getByText('Первый чат')).toBeInTheDocument();
    });

    // Click the chat-row Link → onNavigate → setOpen(false).
    // Note: chat-row links carry role="option" (Phase 19 / D-17 listbox
    // pattern), so we query by `option`, not `link`.
    await user.click(screen.getByRole('option', { name: /Первый чат/ }));

    // Radix unmounts the dialog from the DOM after close-state animation.
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull();
    });
  });

  it('stays open when the project header expand/collapse button is clicked', async () => {
    const user = userEvent.setup();
    render(<Sidebar />, { wrapper: Wrapper });

    await user.click(screen.getByRole('button', { name: 'Открыть боковое меню' }));
    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument();
    });
    // Wait for the project list to hydrate.
    await waitFor(() => {
      expect(screen.getByText('Отзывы')).toBeInTheDocument();
    });

    // Click the «Свернуть «Отзывы»» chevron — local-state toggle, NOT
    // a navigation event. Drawer must stay open.
    const collapseBtn = screen.getByRole('button', { name: /Свернуть «Отзывы»/ });
    await user.click(collapseBtn);

    // Drawer is still in the DOM.
    expect(screen.getByRole('dialog')).toBeInTheDocument();
  });

  it('stays open when the per-row context menu is opened', async () => {
    const user = userEvent.setup();
    const { container } = render(<Sidebar />, { wrapper: Wrapper });

    await user.click(screen.getByRole('button', { name: 'Открыть боковое меню' }));
    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument();
    });
    await waitFor(() => {
      expect(screen.getByText('Первый чат')).toBeInTheDocument();
    });

    // Open the «Меню чата …» dropdown trigger. The drawer must remain open
    // — opening a context menu is not a navigation event.
    const menuTriggers = screen.getAllByRole('button', { name: /Меню чата/ });
    await user.click(menuTriggers[0]);

    // When the DropdownMenu opens, Radix sets aria-hidden=true on background
    // elements (including the parent Sheet) — so `screen.getByRole('dialog')`
    // would not find it. The dialog is still IN THE DOM though; we assert
    // its physical presence via querySelector. The menu role="menu" being
    // present is the proxy for "menu opened".
    const dialog = container.ownerDocument.querySelector('[role="dialog"]');
    expect(dialog).not.toBeNull();
    // Sanity check: the menu is open (this is what made aria-hidden flip).
    expect(screen.getByRole('menu')).toBeInTheDocument();
  });
});
