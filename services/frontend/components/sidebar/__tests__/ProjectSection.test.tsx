import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { ProjectSection } from '../ProjectSection';
import type { Project } from '@/types/project';
import type { Conversation } from '@/lib/conversations';

// Mock next/navigation
const pushMock = vi.fn();
vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: pushMock, back: vi.fn(), replace: vi.fn() }),
}));

// Mock sonner toast.
vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

// Mock axios-based API client. Track project-id passed to POST /conversations.
const apiPost = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    get: vi.fn(() => Promise.resolve({ data: [] })),
    post: (url: string, body: unknown) => apiPost(url, body),
    put: vi.fn(),
    delete: vi.fn(),
  },
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

function makeClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
}

function Wrapper({ children }: { children: ReactNode }) {
  const client = makeClient();
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

function makeConv(id: string, title: string): Conversation {
  return {
    id,
    userId: 'u-1',
    businessId: 'b-1',
    projectId: sampleProject.id,
    title,
    titleStatus: 'auto',
    pinned: false,
    createdAt: '2026-04-18T00:00:00Z',
    updatedAt: '2026-04-18T00:00:00Z',
  };
}

describe('ProjectSection', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    apiPost.mockReset();
    pushMock.mockReset();
  });

  it('renders the project name, chat count, and conversation rows', () => {
    const convs = [makeConv('c-1', 'Chat A'), makeConv('c-2', 'Chat B')];
    render(
      <Wrapper>
        <ProjectSection project={sampleProject} conversations={convs} />
      </Wrapper>
    );

    expect(screen.getByText('Отзывы')).toBeInTheDocument();
    expect(screen.getByText('· 2')).toBeInTheDocument();
    expect(screen.getByText('Chat A')).toBeInTheDocument();
    expect(screen.getByText('Chat B')).toBeInTheDocument();
  });

  it('exposes the per-row + button with the project-specific aria-label', () => {
    render(
      <Wrapper>
        <ProjectSection project={sampleProject} conversations={[]} />
      </Wrapper>
    );

    expect(
      screen.getByRole('button', { name: 'Новый чат в проекте «Отзывы»' })
    ).toBeInTheDocument();
  });

  it('clicking + calls createConversation with the project id and routes to the new chat', async () => {
    apiPost.mockResolvedValue({
      data: {
        id: 'new-conv-id',
        userId: 'u-1',
        businessId: 'b-1',
        projectId: sampleProject.id,
        title: 'Новый диалог',
        titleStatus: 'auto_pending',
        pinned: false,
        createdAt: '2026-04-18T00:00:00Z',
        updatedAt: '2026-04-18T00:00:00Z',
      },
    });

    render(
      <Wrapper>
        <ProjectSection project={sampleProject} conversations={[]} />
      </Wrapper>
    );

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: 'Новый чат в проекте «Отзывы»' }));

    await waitFor(() => {
      expect(apiPost).toHaveBeenCalledWith('/conversations', {
        title: 'Новый диалог',
        projectId: 'p-1',
      });
    });

    await waitFor(() => {
      expect(pushMock).toHaveBeenCalledWith('/chat/new-conv-id');
    });
  });

  it('renders empty-state copy when there are no conversations but keeps the header', () => {
    render(
      <Wrapper>
        <ProjectSection project={sampleProject} conversations={[]} />
      </Wrapper>
    );

    expect(screen.getByText('Отзывы')).toBeInTheDocument();
    expect(screen.getByText('· 0')).toBeInTheDocument();
    expect(screen.getByText('В проекте пока нет чатов')).toBeInTheDocument();
  });

  // Phase 19 / Plan 19-05 / D-17 — roving-tabindex chat-list contract.
  it('chat-row links carry data-roving-item, role="option", and roving tabindex', () => {
    const convs = [
      makeConv('c-1', 'First chat'),
      makeConv('c-2', 'Second chat'),
      makeConv('c-3', 'Third chat'),
    ];
    const { container } = render(
      <Wrapper>
        <ProjectSection project={sampleProject} conversations={convs} />
      </Wrapper>
    );
    const items = container.querySelectorAll('[data-roving-item]');
    expect(items.length).toBe(3);
    items.forEach((item) => {
      expect(item.getAttribute('role')).toBe('option');
    });
    // Initial tabindex distribution: first=0, rest=-1 (single Tab stop).
    expect(items[0].getAttribute('tabindex')).toBe('0');
    expect(items[1].getAttribute('tabindex')).toBe('-1');
    expect(items[2].getAttribute('tabindex')).toBe('-1');
    // The roving container itself has role="listbox".
    expect(container.querySelector('[role="listbox"]')).not.toBeNull();
  });

  it('project-header expand/collapse button is OUTSIDE the roving container (separate Tab stop, D-17)', () => {
    const convs = [makeConv('c-1', 'A chat')];
    const { container } = render(
      <Wrapper>
        <ProjectSection project={sampleProject} conversations={convs} />
      </Wrapper>
    );
    // The collapse button has aria-label «Свернуть «Отзывы»» — it should
    // NOT live inside the role="listbox" container (D-17 — separate Tab stop).
    const listbox = container.querySelector('[role="listbox"]');
    const collapseBtn = screen.getByRole('button', { name: /Свернуть «Отзывы»/ });
    expect(listbox).not.toBeNull();
    expect(listbox?.contains(collapseBtn)).toBe(false);
  });
});
