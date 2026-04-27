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

function makeConv(id: string, title: string, projectId: string | null, pinnedAt: string | null): Conversation {
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

describe('PinnedSection — Phase 19 / D-04 + D-05', () => {
  it('returns null when conversations is empty (D-04 hidden when empty)', () => {
    const { container } = render(
      <Wrapper>
        <PinnedSection conversations={[]} projectsById={{}} />
      </Wrapper>
    );
    // No header, no «Закреплённые» rendered.
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

  it('renders mini ProjectChip for chats with a real projectId (D-05)', () => {
    const convs = [makeConv('c-1', 'Chat in project', 'p-1', '2026-04-27T12:00:00Z')];
    const projectsById = { 'p-1': { id: 'p-1', name: 'Отзывы' } };
    render(
      <Wrapper>
        <PinnedSection conversations={convs} projectsById={projectsById} />
      </Wrapper>
    );
    // The mini chip's project name should be present.
    expect(screen.getByText('Отзывы')).toBeInTheDocument();
  });

  it('renders NO ProjectChip for chats in «Без проекта» (D-05 — projectId == null)', () => {
    const convs = [makeConv('c-1', 'Unbucketed chat', null, '2026-04-27T12:00:00Z')];
    render(
      <Wrapper>
        <PinnedSection conversations={convs} projectsById={{}} />
      </Wrapper>
    );
    expect(screen.getByText('Unbucketed chat')).toBeInTheDocument();
    // The «Без проекта» literal label MUST NOT appear inside a pinned row
    // (we only show that label inside the UnassignedBucket header).
    expect(screen.queryByText('Без проекта')).not.toBeInTheDocument();
  });

  it('preserves caller-supplied order (caller pre-sorts by pinnedAt desc per D-03)', () => {
    // Caller has already sorted by pinnedAt desc — most-recent first.
    const convs = [
      makeConv('c-newest', 'Newer pinned', null, '2026-04-27T12:00:00Z'),
      makeConv('c-older', 'Older pinned', null, '2026-04-26T12:00:00Z'),
    ];
    render(
      <Wrapper>
        <PinnedSection conversations={convs} projectsById={{}} />
      </Wrapper>
    );
    const links = screen.getAllByRole('link');
    expect(links[0]).toHaveTextContent('Newer pinned');
    expect(links[1]).toHaveTextContent('Older pinned');
  });
});
