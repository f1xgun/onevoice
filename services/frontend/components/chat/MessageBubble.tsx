import { ToolCallsBlock } from './ToolCallsBlock';
import type { Message } from '@/types/chat';

export function MessageBubble({ message }: { message: Message }) {
  const isUser = message.role === 'user';

  return (
    <div className={`flex ${isUser ? 'justify-end' : 'justify-start'} mb-4`}>
      <div className={`max-w-[75%] ${isUser ? 'order-2' : 'order-1'}`}>
        <div
          className={`rounded-2xl px-4 py-3 text-sm ${
            isUser
              ? 'rounded-br-sm bg-blue-600 text-white'
              : 'rounded-bl-sm border border-gray-200 bg-white text-gray-800'
          }`}
        >
          {message.status === 'streaming' && !message.content ? (
            <span className="flex gap-1">
              <span className="h-2 w-2 animate-bounce rounded-full bg-gray-400 [animation-delay:0ms]" />
              <span className="h-2 w-2 animate-bounce rounded-full bg-gray-400 [animation-delay:150ms]" />
              <span className="h-2 w-2 animate-bounce rounded-full bg-gray-400 [animation-delay:300ms]" />
            </span>
          ) : (
            <p className="whitespace-pre-wrap">{message.content}</p>
          )}
          {!isUser && message.toolCalls && message.toolCalls.length > 0 && (
            <ToolCallsBlock toolCalls={message.toolCalls} />
          )}
        </div>
      </div>
    </div>
  );
}
