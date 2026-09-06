import type * as React from 'react';
import { act, render, screen } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { useBusinessStore } from '@/lib/stores/business';
import ConversationPage from '../page';

const { replace, unmount, renderChat } = vi.hoisted(() => ({
  replace: vi.fn(),
  unmount: vi.fn(),
  renderChat: vi.fn(),
}));
vi.mock('react', async (original) => ({
  ...(await original<typeof React>()),
  use: (value: unknown) => value,
}));
vi.mock('next/navigation', () => ({
  useRouter: () => ({ replace }),
  useSearchParams: () => new URLSearchParams(),
}));
vi.mock('@/hooks/useHighlightMessage', () => ({ useHighlightMessage: vi.fn() }));
vi.mock('@/components/chat/ChatWindow', async () => {
  const { useEffect } = await import('react');
  return {
    ChatWindow: function ChatWindow() {
      renderChat();
      useEffect(() => unmount, []);
      return <div data-testid="chat" />;
    },
  };
});

it('unmounts the old conversation before rendering the new tenant', () => {
  useBusinessStore.getState().setActive('a');
  render(<ConversationPage params={{ id: 'chat-a' } as unknown as Promise<{ id: string }>} />);
  expect(screen.getByTestId('chat')).toBeVisible();
  const previousRenders = renderChat.mock.calls.length;
  act(() => useBusinessStore.getState().setActive('b'));
  expect(screen.queryByTestId('chat')).not.toBeInTheDocument();
  expect(unmount).toHaveBeenCalledOnce();
  expect(renderChat).toHaveBeenCalledTimes(previousRenders);
  expect(replace).toHaveBeenCalledWith('/chat');
});
