import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { toast } from 'sonner';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { MoveChatMenuItem } from '../MoveChatMenuItem';
import type { Project } from '@/types/project';

// Mock sonner — keep the default (success/error) signatures.
vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

// Mock axios-based API: listProjects + moveConversation.
const listProjects = vi.fn<[], Promise<Project[]>>();
const postMock = vi.fn<[string, unknown], Promise<{ data: unknown }>>();
vi.mock('@/lib/api', () => ({
  api: {
    get: (url: string) => {
      if (url === '/projects') return listProjects().then((data) => ({ data }));
      return Promise.resolve({ data: null });
    },
    post: (url: string, body: unknown) => postMock(url, body),
    put: vi.fn(),
    delete: vi.fn(),
  },
}));

const projectA: Project = {
  id: 'p-alpha',
  businessId: 'b-1',
  name: 'Альфа',
  description: '',
  systemPrompt: '',
  whitelistMode: 'inherit',
  allowedTools: [],
  quickActions: [],
  createdAt: '2026-04-18T00:00:00Z',
  updatedAt: '2026-04-18T00:00:00Z',
};

const projectB: Project = { ...projectA, id: 'p-beta', name: 'Бета' };

function makeClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: 0 } },
  });
}

function Wrapper({ children }: { children: ReactNode }) {
  const client = makeClient();
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

function renderWithTrigger(currentProjectId: string | null) {
  return render(
    <Wrapper>
      <DropdownMenu>
        <DropdownMenuTrigger>Меню</DropdownMenuTrigger>
        <DropdownMenuContent>
          <MoveChatMenuItem conversationId="c-1" currentProjectId={currentProjectId} />
        </DropdownMenuContent>
      </DropdownMenu>
    </Wrapper>
  );
}

async function openMenu(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole('button', { name: 'Меню' }));
  // Focus the submenu trigger and open the submenu via ArrowRight (jsdom-friendly).
  const trigger = await screen.findByText('Переместить в…');
  trigger.focus();
  await user.keyboard('{ArrowRight}');
}

describe('MoveChatMenuItem', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    listProjects.mockReset();
    postMock.mockReset();
    listProjects.mockResolvedValue([projectB, projectA]);
  });

  it('moves to «Без проекта» and shows a 5s success toast', async () => {
    postMock.mockResolvedValue({
      data: {
        id: 'c-1',
        userId: 'u-1',
        businessId: 'b-1',
        projectId: null,
        title: 'Chat',
        titleStatus: 'auto',
        pinned: false,
        createdAt: '2026-04-18T00:00:00Z',
        updatedAt: '2026-04-18T00:00:00Z',
      },
    });

    const user = userEvent.setup();
    renderWithTrigger('p-alpha');
    await openMenu(user);

    const unassigned = await screen.findByText('Без проекта');
    await user.click(unassigned);

    await waitFor(() => {
      expect(postMock).toHaveBeenCalledWith('/conversations/c-1/move', {
        projectId: null,
      });
    });

    await waitFor(() => {
      expect(toast.success).toHaveBeenCalledWith(
        'Чат перемещён в «Без проекта»',
        expect.objectContaining({
          duration: 5000,
          action: expect.objectContaining({ label: 'Отменить' }),
        })
      );
    });
  });

  it('moves to the chosen project with its id and sorts entries by name', async () => {
    postMock.mockResolvedValue({
      data: {
        id: 'c-1',
        userId: 'u-1',
        businessId: 'b-1',
        projectId: 'p-alpha',
        title: 'Chat',
        titleStatus: 'auto',
        pinned: false,
        createdAt: '2026-04-18T00:00:00Z',
        updatedAt: '2026-04-18T00:00:00Z',
      },
    });

    const user = userEvent.setup();
    renderWithTrigger(null);
    await openMenu(user);

    // Alphabetical sorting (ru locale): "Альфа" before "Бета".
    const alpha = await screen.findByText('Альфа');
    await user.click(alpha);

    await waitFor(() => {
      expect(postMock).toHaveBeenCalledWith('/conversations/c-1/move', {
        projectId: 'p-alpha',
      });
    });

    await waitFor(() => {
      expect(toast.success).toHaveBeenCalledWith(
        'Чат перемещён в «Альфа»',
        expect.objectContaining({ duration: 5000 })
      );
    });
  });

  it('Undo button calls move with the previousProjectId', async () => {
    postMock.mockResolvedValue({
      data: {
        id: 'c-1',
        userId: 'u-1',
        businessId: 'b-1',
        projectId: 'p-beta',
        title: 'Chat',
        titleStatus: 'auto',
        pinned: false,
        createdAt: '2026-04-18T00:00:00Z',
        updatedAt: '2026-04-18T00:00:00Z',
      },
    });

    const user = userEvent.setup();
    renderWithTrigger('p-alpha');
    await openMenu(user);

    const beta = await screen.findByText('Бета');
    await user.click(beta);

    // Collect the action handler that sonner received.
    await waitFor(() => {
      expect(toast.success).toHaveBeenCalled();
    });
    const [, options] = (toast.success as unknown as { mock: { calls: unknown[][] } }).mock
      .calls[0] as [string, { action: { onClick: () => void } }];

    // Invoke Undo — should fire a second move back to the original project.
    options.action.onClick();

    await waitFor(() => {
      expect(postMock).toHaveBeenNthCalledWith(2, '/conversations/c-1/move', {
        projectId: 'p-alpha',
      });
    });
  });
});
