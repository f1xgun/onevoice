import { act, renderHook } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { useChatComposer } from '../useChatComposer';

it('keeps a post prompt editable without sending it on navigation', () => {
  const sendMessage = vi.fn();
  const { result } = renderHook(() =>
    useChatComposer({
      messages: [],
      disabled: false,
      sendMessage,
      initialPrompt: 'Repeat this post',
    })
  );
  expect(result.current.input).toBe('Repeat this post');
  act(() => result.current.prefill('Edited before sending'));
  expect(result.current.input).toBe('Edited before sending');
  expect(sendMessage).not.toHaveBeenCalled();
});
