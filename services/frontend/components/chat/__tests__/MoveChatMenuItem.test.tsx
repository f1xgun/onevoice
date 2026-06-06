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

// Mock useBusinessStore so hooks get a stable activeBusinessId.
vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (selector: (s: { activeBusinessId: string | null }) => unknown) =>
    selector({ activeBusinessId: 'test-biz-id' }),
}));

// Mock bizApi — projects.ts and conversations.ts now use bizApi(bizId).verb(path).
const bizApiGet = vi.fn();
const bizApiPost = vi.fn();
vi.mock('@/lib/api/business-api', () => ({
  bizApi: (bizId: string) => ({
    get: (path: string, config?: unknown) => bizApiGet(bizId, path, config),
    post: (path: string, body?: unknown) => bizApiPost(bizId, path, body),
    put: vi.fn(),
    delete: vi.fn(),
  }),
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
  const trigger = await screen.findByText('Переместить в…');
  trigger.focus();
  await user.keyboard('{ArrowRight}');
}

describe('MoveChatMenuItem', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    bizApiGet.mockReset();
    bizApiPost.mockReset();
    bizApiGet.mockImplementation((_bizId: string, path: string) => {
      if (path === '/projects') {
        return Promise.resolve({ data: [projectB, projectA] });
      }
      return Promise.resolve({ data: null });
    });
  });

  it('moves to «Без проекта» and shows a 5s success toast', async () => {
    bizApiPost.mockResolvedValue({
      data: {
        id: 'c-1',
        userId: 'u-1',
        businessId: 'b-1',
        projectId: null,
        title: 'Chat',
        titleStatus: 'auto',
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
      expect(bizApiPost).toHaveBeenCalledWith('test-biz-id', '/conversations/c-1/move', {
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
    bizApiPost.mockResolvedValue({
      data: {
        id: 'c-1',
        userId: 'u-1',
        businessId: 'b-1',
        projectId: 'p-alpha',
        title: 'Chat',
        titleStatus: 'auto',
        createdAt: '2026-04-18T00:00:00Z',
        updatedAt: '2026-04-18T00:00:00Z',
      },
    });

    const user = userEvent.setup();
    renderWithTrigger(null);
    await openMenu(user);

    const alpha = await screen.findByText('Альфа');
    await user.click(alpha);

    await waitFor(() => {
      expect(bizApiPost).toHaveBeenCalledWith('test-biz-id', '/conversations/c-1/move', {
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
    bizApiPost.mockResolvedValue({
      data: {
        id: 'c-1',
        userId: 'u-1',
        businessId: 'b-1',
        projectId: 'p-beta',
        title: 'Chat',
        titleStatus: 'auto',
        createdAt: '2026-04-18T00:00:00Z',
        updatedAt: '2026-04-18T00:00:00Z',
      },
    });

    const user = userEvent.setup();
    renderWithTrigger('p-alpha');
    await openMenu(user);

    const beta = await screen.findByText('Бета');
    await user.click(beta);

    await waitFor(() => {
      expect(toast.success).toHaveBeenCalled();
    });
    const [, options] = (toast.success as unknown as { mock: { calls: unknown[][] } }).mock
      .calls[0] as [string, { action: { onClick: () => void } }];

    options.action.onClick();

    await waitFor(() => {
      expect(bizApiPost).toHaveBeenNthCalledWith(2, 'test-biz-id', '/conversations/c-1/move', {
        projectId: 'p-alpha',
      });
    });
  });
});
