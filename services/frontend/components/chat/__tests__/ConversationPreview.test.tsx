import { afterEach, expect, it, vi } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import { useConversationsQuery, conversationsQueryKey } from '@/hooks/useConversations';
import ru from '@/messages/ru.json';
import { PinnedSection } from '@/components/sidebar/PinnedSection';
import { ProjectSection } from '@/components/sidebar/ProjectSection';
import { UnassignedBucket } from '@/components/sidebar/UnassignedBucket';
import type { Conversation } from '@/lib/conversations';

import { ConversationItem } from '../ConversationItem';

vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: vi.fn() }),
  usePathname: () => '/chat',
}));

const get = vi.fn();
let businessId = 'org-a';
vi.mock('@/lib/api/business-api', () => ({
  bizApi: (id: string) => ({ get: (...args: unknown[]) => get(id, ...args) }),
}));
vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (selector: (state: { activeBusinessId: string }) => unknown) =>
    selector({ activeBusinessId: businessId }),
}));

afterEach(() => {
  get.mockReset();
  businessId = 'org-a';
});

function ConversationList() {
  const { data = [] } = useConversationsQuery();
  return data.map((conv) => (
    <ConversationItem
      key={conv.id}
      conv={conv}
      onOpen={vi.fn()}
      onRename={vi.fn()}
      onDelete={vi.fn()}
      onRegenerateTitle={vi.fn()}
    />
  ));
}

function rows(preview: string) {
  return [preview, ''].map((text, i) => ({
    id: `chat-${i}`,
    title: `My title ${i}`,
    titleStatus: 'manual',
    createdAt: '2026-09-06T10:00:00Z',
    preview: text,
  }));
}

it('renders previews and empty chats from one list request and refreshes the organization cache', async () => {
  get.mockResolvedValue({ data: rows('Saturday at 11:00') });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  function List() {
    return (
      <QueryClientProvider client={client}>
        <ConversationList />
      </QueryClientProvider>
    );
  }
  const { rerender } = render(<List />);
  expect(await screen.findByText('Saturday at 11:00')).toBeVisible();
  expect(screen.getByText(ru.chat.rowMenu.noMessages)).toBeVisible();
  expect(screen.getByText('My title 0')).toBeVisible();
  expect(get).toHaveBeenCalledTimes(1);
  expect(get).toHaveBeenLastCalledWith('org-a', '/conversations', { params: { limit: 100 } });

  get.mockResolvedValue({ data: rows('Updated reply') });
  await act(async () => {
    await client.invalidateQueries({ queryKey: conversationsQueryKey('org-a') });
  });
  expect(await screen.findByText('Updated reply')).toBeVisible();
  expect(get).toHaveBeenCalledTimes(2);

  get.mockResolvedValue({ data: rows('Sunday at 12:00') });
  businessId = 'org-b';
  rerender(<List />);
  expect(screen.queryByText('Updated reply')).toBeNull();
  expect(await screen.findByText('Sunday at 12:00')).toBeVisible();
  await waitFor(() => expect(get).toHaveBeenCalledTimes(3));
  expect(get).toHaveBeenLastCalledWith('org-b', '/conversations', { params: { limit: 100 } });
  client.clear();
});

it('shows an unavailable preview for an older response without fetching history', () => {
  render(
    <ConversationItem
      conv={{ id: 'legacy', title: 'Legacy title', createdAt: '2026-09-06T10:00:00Z' }}
      onOpen={vi.fn()}
      onRename={vi.fn()}
      onDelete={vi.fn()}
      onRegenerateTitle={vi.fn()}
    />
  );
  expect(screen.getByText(ru.chat.rowMenu.previewUnavailable)).toBeVisible();
  expect(get).not.toHaveBeenCalled();
});
it('renders the list preview in pinned, project and unassigned sidebar rows', () => {
  const conv: Conversation = {
    id: 'sidebar-chat',
    userId: 'user',
    businessId: 'org-a',
    projectId: null,
    title: 'Sidebar chat',
    titleStatus: 'manual',
    preview: 'Shared server preview',
    createdAt: '2026-09-06T10:00:00Z',
    updatedAt: '2026-09-06T10:00:00Z',
  };
  const client = new QueryClient();
  render(
    <QueryClientProvider client={client}>
      <PinnedSection conversations={[conv]} projectsById={{}} />
      <ProjectSection
        conversations={[conv]}
        project={{
          id: 'project',
          businessId: 'org-a',
          name: 'Project',
          description: '',
          systemPrompt: '',
          whitelistMode: 'inherit',
          allowedTools: [],
          quickActions: [],
          createdAt: conv.createdAt,
          updatedAt: conv.updatedAt,
        }}
      />
      <UnassignedBucket conversations={[conv]} />
    </QueryClientProvider>
  );
  expect(screen.getAllByText('Shared server preview')).toHaveLength(3);
  expect(get.mock.calls.some(([, path]) => String(path).includes('/messages'))).toBe(false);
  client.clear();
});
