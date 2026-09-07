import { afterEach, expect, it, vi } from 'vitest';
import { act, render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ConversationItem } from '../ConversationItem';
import { ConversationPreview, conversationPreview } from '../ConversationPreview';
import ru from '@/messages/ru.json';

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
  vi.unstubAllGlobals();
  get.mockReset();
  businessId = 'org-a';
});

function observe() {
  let intersect: IntersectionObserverCallback = () => {};
  vi.stubGlobal(
    'IntersectionObserver',
    class {
      constructor(callback: IntersectionObserverCallback) {
        intersect = callback;
      }
      observe() {}
      disconnect() {}
    }
  );
  return () =>
    act(() =>
      intersect([{ isIntersecting: true } as IntersectionObserverEntry], {} as IntersectionObserver)
    );
}

it('takes the last readable message, excludes tool/system data and bounds text', () => {
  expect(
    conversationPreview([
      { role: 'user', content: ' First ' },
      { role: 'assistant', content: 'New\n hours' },
      { role: 'tool', content: 'internal arguments' },
    ])
  ).toBe('New hours');
  expect(conversationPreview([{ role: 'system', content: 'internal' }])).toBe('');
  expect(conversationPreview([{ role: 'user', content: 'x'.repeat(200) }])).toHaveLength(161);
});

it('loads only visible rows, preserves manual titles, and isolates organization caches', async () => {
  const enter = observe();
  get.mockImplementation((id: string) =>
    Promise.resolve({
      data: {
        messages: [
          { role: 'user', content: id === 'org-a' ? 'Saturday at 11:00' : 'Sunday at 12:00' },
        ],
      },
    })
  );
  const client = new QueryClient();
  function Row() {
    return (
      <QueryClientProvider client={client}>
        <ConversationItem
          conv={{
            id: 'chat',
            title: 'My title',
            titleStatus: 'manual',
            createdAt: '2026-09-06T10:00:00Z',
          }}
          onOpen={vi.fn()}
          onRename={vi.fn()}
          onDelete={vi.fn()}
          onRegenerateTitle={vi.fn()}
        />
      </QueryClientProvider>
    );
  }
  const { rerender } = render(<Row />);
  expect(get).not.toHaveBeenCalled();
  enter();
  expect(await screen.findByText('Saturday at 11:00')).toBeVisible();
  expect(screen.getByText('My title')).toBeVisible();
  businessId = 'org-b';
  rerender(<Row />);
  expect(screen.queryByText('Saturday at 11:00')).toBeNull();
  expect(await screen.findByText('Sunday at 12:00')).toBeVisible();
  expect(get).toHaveBeenCalledWith(
    'org-b',
    '/conversations/chat/messages',
    expect.objectContaining({ signal: expect.any(AbortSignal) })
  );
});

it.each([false, true])(
  'distinguishes empty history from failed preview: error=%s',
  async (error) => {
    const enter = observe();
    if (error) get.mockRejectedValue(new Error('offline'));
    else get.mockResolvedValue({ data: { messages: [] } });
    render(
      <QueryClientProvider client={new QueryClient()}>
        <ConversationPreview conversationId="chat" />
      </QueryClientProvider>
    );
    enter();
    expect(
      await screen.findByText(
        error ? ru.chat.rowMenu.previewUnavailable : ru.chat.rowMenu.noMessages
      )
    ).toBeVisible();
  }
);
