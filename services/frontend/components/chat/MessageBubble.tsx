import Markdown from 'react-markdown';
import { ToolCallsBlock } from './ToolCallsBlock';
import type { Message } from '@/types/chat';

export function MessageBubble({ message }: { message: Message }) {
  const isUser = message.role === 'user';

  return (
    <div
      data-message-id={message.id}
      className={`flex ${isUser ? 'justify-end' : 'justify-start'} mb-4`}
    >
      <div className={`max-w-[75%] ${isUser ? 'order-2' : 'order-1'}`}>
        <div
          className={`rounded-2xl px-4 py-3 text-sm ${
            isUser
              ? 'rounded-br-sm bg-info text-paper'
              : 'rounded-bl-sm border border-line bg-paper-raised text-ink'
          }`}
        >
          {message.status === 'streaming' && !message.content ? (
            <span className="flex gap-1">
              <span className="h-2 w-2 animate-bounce rounded-full bg-ink-faint [animation-delay:0ms]" />
              <span className="h-2 w-2 animate-bounce rounded-full bg-ink-faint [animation-delay:150ms]" />
              <span className="h-2 w-2 animate-bounce rounded-full bg-ink-faint [animation-delay:300ms]" />
            </span>
          ) : isUser ? (
            <p className="whitespace-pre-wrap">{message.content}</p>
          ) : (
            <div className="prose prose-sm max-w-none prose-p:my-1 prose-ol:my-1 prose-ul:my-1 prose-li:my-0.5">
              <Markdown>{message.content}</Markdown>
            </div>
          )}
          {!isUser && message.toolCalls && message.toolCalls.length > 0 && (
            <ToolCallsBlock toolCalls={message.toolCalls} />
          )}
        </div>
      </div>
    </div>
  );
}
