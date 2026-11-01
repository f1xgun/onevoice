import { createElement, type ReactNode } from 'react';
import { act, renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, expect, it, vi } from 'vitest';
import { useConversationFlow } from '../useConversationFlow';
import { useAuthStore } from '@/lib/auth';
import { useBusinessStore } from '@/lib/stores/business';
import { sseLine } from '@/test-utils/sse-mock';

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
afterEach(() => vi.unstubAllGlobals());
it('does not fetch or send on an empty list mount and sends the explicit first message once to its created id', async () => {
  useAuthStore.setState({ accessToken: 'test-token', isAuthenticated: true });
  useBusinessStore.setState({ activeBusinessId: 'business' });
  const fetch = vi.fn().mockResolvedValue(
    new Response(sseLine({ type: 'text', content: 'Prepared' }) + sseLine({ type: 'done' }), {
      headers: { 'Content-Type': 'text/event-stream' },
    })
  );
  vi.stubGlobal('fetch', fetch);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const wrapper = ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client }, children);
  const { result, unmount } = renderHook(() => useConversationFlow({ conversationId: '' }), {
    wrapper,
  });
  await waitFor(() => expect(result.current.isLoading).toBe(false));
  expect(fetch).not.toHaveBeenCalled();
  const accepted = vi.fn();
  const photo = {
    type: 'image' as const,
    url: '/media/photo.png',
    name: 'Photo.png',
    mimeType: 'image/png',
  };
  await act(async () =>
    result.current.sendMessage('Use my photo', accepted, {
      conversationId: 'created',
      attachments: [photo],
    })
  );
  expect(fetch).toHaveBeenCalledOnce();
  expect(String(fetch.mock.calls[0][0])).toBe('/api/v1/businesses/business/chat/created');
  expect(JSON.parse(fetch.mock.calls[0][1].body)).toMatchObject({
    message: 'Use my photo',
    attachments: [photo],
  });
  expect(accepted).toHaveBeenCalledOnce();
  expect(result.current.messages[0].attachments).toEqual([photo]);
  unmount();
  renderHook(() => useConversationFlow({ conversationId: '' }), { wrapper });
  expect(fetch).toHaveBeenCalledOnce();
});
