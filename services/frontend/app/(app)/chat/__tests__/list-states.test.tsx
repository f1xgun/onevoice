import { act, renderHook } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { useChatComposer } from '@/hooks/useChatComposer';

it('keeps a separately attached image out of the visible instruction text', async () => {
  const sendMessage = vi.fn().mockResolvedValue(undefined);
  const { result } = renderHook(() =>
    useChatComposer({ messages: [], disabled: false, sendMessage, initialPrompt: 'Use my photo' })
  );
  const photo = {
    type: 'image' as const,
    url: '/media/photo.png',
    name: 'Photo.png',
    mimeType: 'image/png',
  };
  act(() => result.current.setAttachment(photo));
  await act(async () => result.current.handleSend());
  expect(sendMessage).toHaveBeenCalledWith('Use my photo', expect.any(Function), {
    attachments: [photo],
  });
  expect(result.current.input).toBe('Use my photo');
  act(() => sendMessage.mock.calls[0][1]());
  expect(result.current.input).toBe('');
  expect(result.current.attachment).toBeUndefined();
});

it('carries a post prompt only into a new conversation in the matching business', async () => {
  h.activeBusinessId = 'test-org';
  h.push.mockReset();
  vi.mocked(listConversations).mockResolvedValue([]);
  const post = vi.fn().mockResolvedValue({ data: { id: 'draft-conversation' } });
  vi.mocked(bizApi).mockReturnValue({ post } as unknown as ReturnType<typeof bizApi>);
  window.history.replaceState({}, '', '/chat?prompt=Editable+post&businessId=test-org');
  renderPage();
  await waitFor(() =>
    expect(h.push).toHaveBeenCalledWith(
      '/chat/draft-conversation?prompt=Editable+post&businessId=test-org'
    )
  );
  expect(post).toHaveBeenCalledTimes(1);
  expect(post.mock.calls[0][0]).toBe('/conversations');
  expect(post.mock.calls[0][1]).not.toHaveProperty('message');
  window.history.replaceState({}, '', '/chat');
});
