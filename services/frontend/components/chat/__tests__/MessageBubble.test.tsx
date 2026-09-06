import { describe, it, expect, afterEach } from 'vitest';
import { render, screen, cleanup } from '@testing-library/react';
import { MessageBubble } from '@/components/chat/MessageBubble';
import type { Message } from '@/types/chat';

// The visible typing caption reuses chat.window.typingAria — "OneVoice печатает"
// in the default (ru) test locale.
const TYPING_CAPTION = 'OneVoice печатает';

afterEach(() => cleanup());

function assistant(partial: Partial<Message>): Message {
  return { id: 'a1', role: 'assistant', content: '', ...partial };
}

describe('MessageBubble — typing / working indicator', () => {
  it('shows the typing indicator for an empty streaming assistant bubble', () => {
    render(<MessageBubble message={assistant({ status: 'streaming', content: '' })} />);
    expect(screen.getAllByText(TYPING_CAPTION).length).toBeGreaterThan(0);
  });

  it('keeps a working footer visible while streaming AFTER content has arrived', () => {
    // Regression: the indicator used to vanish the moment the first token
    // rendered, so a long LLM/tool wait looked frozen. The footer must persist.
    render(
      <MessageBubble message={assistant({ status: 'streaming', content: 'Partial answer…' })} />
    );
    expect(screen.getByText('Partial answer…')).toBeInTheDocument();
    expect(screen.getAllByText(TYPING_CAPTION).length).toBeGreaterThan(0);
  });

  it('shows no working footer once the message is done', () => {
    render(<MessageBubble message={assistant({ status: 'done', content: 'Final answer.' })} />);
    expect(screen.getByText('Final answer.')).toBeInTheDocument();
    expect(screen.queryByText(TYPING_CAPTION)).not.toBeInTheDocument();
  });

  it('renders nothing for an empty done assistant message with no tool calls', () => {
    const { container } = render(
      <MessageBubble message={assistant({ status: 'done', content: '' })} />
    );
    expect(container).toBeEmptyDOMElement();
  });

  it('does not show a typing indicator for user messages', () => {
    render(
      <MessageBubble message={{ id: 'u1', role: 'user', content: 'Hello', status: 'done' }} />
    );
    expect(screen.queryByText(TYPING_CAPTION)).not.toBeInTheDocument();
  });

  it('renders a localized error notice (not [Error: ...]) when the turn ended on an error frame', () => {
    render(
      <MessageBubble
        message={assistant({
          status: 'done',
          content: '',
          errorCode: 'max_iterations',
          errorDetail: 'max iterations (10) reached',
        })}
      />
    );
    expect(screen.getByText(/слишком сложным/)).toBeInTheDocument();
    expect(screen.queryByText(/\[Error:/)).not.toBeInTheDocument();
  });
});

it('shows authors beside owner text and semantic Markdown', () => {
  render(
    <>
      <MessageBubble message={{ id: 'u', role: 'user', content: 'Поручение', status: 'done' }} />
      <MessageBubble
        message={assistant({ status: 'done', content: '## Черновик\n\n**Проверить** текст' })}
      />
    </>
  );
  expect(screen.getByText('Вы')).toBeInTheDocument();
  expect(screen.getByText('OneVoice')).toBeInTheDocument();
  expect(screen.getByRole('heading', { name: 'Черновик' })).toBeInTheDocument();
  expect(screen.getByText('Проверить').tagName).toBe('STRONG');
});
