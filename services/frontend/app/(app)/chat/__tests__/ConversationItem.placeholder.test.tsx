import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import { ConversationItem } from '@/components/chat/ConversationItem';

// Phase 18 / TITLE-01 / D-09:
// Sidebar / chat-list rows render the verbatim Russian placeholder
// "Новый диалог" whenever conv.title === '' OR titleStatus === 'auto_pending'.
// NO shimmer / skeleton / animation — CONTEXT.md "Sidebar Pending UX".

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

// Stub axios-based api client so MoveChatMenuItem's projects query (which
// renders inside the kebab dropdown's portal) does not blow up at mount.
vi.mock('@/lib/api', () => ({
  api: {
    get: () => Promise.resolve({ data: [] }),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  },
}));

function makeClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: 0 } },
  });
}

function Wrapper({ children }: { children: ReactNode }) {
  const client = makeClient();
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

interface TestConv {
  id: string;
  title: string;
  titleStatus?: 'auto_pending' | 'auto' | 'manual';
  createdAt: string;
  projectId?: string | null;
}

const baseConv: TestConv = {
  id: 'c-1',
  title: '',
  titleStatus: 'auto_pending',
  createdAt: '2026-04-26T10:00:00Z',
  projectId: null,
};

function renderItem(conv: TestConv) {
  return render(
    <Wrapper>
      <ConversationItem
        conv={conv}
        onOpen={vi.fn()}
        onRename={vi.fn()}
        onDelete={vi.fn()}
        onRegenerateTitle={vi.fn()}
      />
    </Wrapper>
  );
}

describe('ConversationItem placeholder (D-09)', () => {
  it("renders 'Новый диалог' when title is empty (auto_pending status)", () => {
    renderItem({ ...baseConv, title: '', titleStatus: 'auto_pending' });
    expect(screen.getByText('Новый диалог')).toBeInTheDocument();
  });

  it("renders 'Новый диалог' when title is empty and titleStatus is undefined", () => {
    renderItem({ ...baseConv, title: '', titleStatus: undefined });
    expect(screen.getByText('Новый диалог')).toBeInTheDocument();
  });

  it("renders 'Новый диалог' when titleStatus is 'auto_pending' EVEN WITH a non-empty title", () => {
    // D-09 invariant: status='auto_pending' wins over a stale stored title.
    renderItem({
      ...baseConv,
      title: 'Stale title leaking from a previous render',
      titleStatus: 'auto_pending',
    });
    expect(screen.getByText('Новый диалог')).toBeInTheDocument();
    expect(
      screen.queryByText('Stale title leaking from a previous render')
    ).not.toBeInTheDocument();
  });

  it("renders the actual title when titleStatus is 'auto' with non-empty title", () => {
    renderItem({ ...baseConv, title: 'Запланировать пост', titleStatus: 'auto' });
    expect(screen.getByText('Запланировать пост')).toBeInTheDocument();
    expect(screen.queryByText('Новый диалог')).not.toBeInTheDocument();
  });

  it("renders the actual title when titleStatus is 'manual'", () => {
    renderItem({ ...baseConv, title: 'My custom title', titleStatus: 'manual' });
    expect(screen.getByText('My custom title')).toBeInTheDocument();
    expect(screen.queryByText('Новый диалог')).not.toBeInTheDocument();
  });
});
