import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { PinnedSection } from '../PinnedSection';
import type { Conversation } from '@/lib/conversations';

// Mock next/navigation — PinnedSection uses next/link which references router.
const pushMock = vi.fn();
vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: pushMock, back: vi.fn(), replace: vi.fn() }),
  usePathname: () => '/chat',
}));

vi.mock('@/lib/api', () => ({
  api: {
    get: vi.fn(() => Promise.resolve({ data: [] })),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  },
}));

function makeClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function Wrapper({ children }: { children: ReactNode }) {
  const client = makeClient();
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

function makeConv(
  id: string,
  title: string,
  projectId: string | null,
  pinnedAt: string | null
): Conversation {
  return {
    id,
    userId: 'u-1',
    businessId: 'b-1',
    projectId,
    title,
    titleStatus: 'auto',
    pinnedAt,
    createdAt: '2026-04-18T00:00:00Z',
    updatedAt: '2026-04-18T00:00:00Z',
  };
}

describe('PinnedSection', () => {
  it('returns null when conversations is empty (hidden when empty)', () => {
    const { container } = render(
      <Wrapper>
        <PinnedSection conversations={[]} projectsById={{}} />
      </Wrapper>
    );
    expect(container.firstChild).toBeNull();
    expect(screen.queryByText('Закреплённые')).not.toBeInTheDocument();
  });

  it('renders header «Закреплённые» + chat row when there is at least one pinned chat', () => {
    const convs = [makeConv('c-1', 'My pinned chat', null, '2026-04-27T12:00:00Z')];
    render(
      <Wrapper>
        <PinnedSection conversations={convs} projectsById={{}} />
      </Wrapper>
    );
    expect(screen.getByText('Закреплённые')).toBeInTheDocument();
    expect(screen.getByText('My pinned chat')).toBeInTheDocument();
    expect(screen.getByText('· 1')).toBeInTheDocument();
  });

  it('renders mini ProjectChip for chats with a real projectId', () => {
    const convs = [makeConv('c-1', 'Chat in project', 'p-1', '2026-04-27T12:00:00Z')];
    const projectsById = { 'p-1': { id: 'p-1', name: 'Отзывы' } };
    render(
      <Wrapper>
        <PinnedSection conversations={convs} projectsById={projectsById} />
      </Wrapper>
    );
    expect(screen.getByText('Отзывы')).toBeInTheDocument();
  });

  it('renders NO ProjectChip for chats in «Без проекта» (projectId == null)', () => {
    const convs = [makeConv('c-1', 'Unbucketed chat', null, '2026-04-27T12:00:00Z')];
    render(
      <Wrapper>
        <PinnedSection conversations={convs} projectsById={{}} />
      </Wrapper>
    );
    expect(screen.getByText('Unbucketed chat')).toBeInTheDocument();
    expect(screen.queryByText('Без проекта')).not.toBeInTheDocument();
  });

  it('preserves caller-supplied order (caller pre-sorts by pinnedAt desc)', () => {
    const convs = [
      makeConv('c-newest', 'Newer pinned', null, '2026-04-27T12:00:00Z'),
      makeConv('c-older', 'Older pinned', null, '2026-04-26T12:00:00Z'),
    ];
    render(
      <Wrapper>
        <PinnedSection conversations={convs} projectsById={{}} />
      </Wrapper>
    );
    const options = screen.getAllByRole('link');
    expect(options[0]).toHaveTextContent('Newer pinned');
    expect(options[1]).toHaveTextContent('Older pinned');
  });

  it('chat-row links carry data-roving-item and roving tabindex (no listbox role)', () => {
    const convs = [
      makeConv('c-1', 'First', null, '2026-04-27T12:00:00Z'),
      makeConv('c-2', 'Second', null, '2026-04-26T12:00:00Z'),
    ];
    const { container } = render(
      <Wrapper>
        <PinnedSection conversations={convs} projectsById={{}} />
      </Wrapper>
    );
    const items = container.querySelectorAll('[data-roving-item]');
    expect(items.length).toBe(2);
    expect(items[0].getAttribute('role')).toBeNull();
    expect(items[0].getAttribute('tabindex')).toBe('0');
    expect(items[1].getAttribute('tabindex')).toBe('-1');
    expect(container.querySelector('[role="listbox"]')).toBeNull();
  });
});
