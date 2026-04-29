// components/chat/MessageBubble.tsx — OneVoice (Linen) chat message
//
// Design contract from design_handoff_onevoice 2/mocks/mock-ai-chat.jsx:
//   - User messages: right-aligned, NO bubble background, plain ink text
//     (a gentle right-shift, no dark fill, no platform-tinted bubble).
//   - Assistant messages: left-aligned, prefixed with the OneVoice
//     ChannelMark, body on a quiet paper-raised card with a 1 px line
//     border. Markdown rendered inside.
//   - Streaming dots: bouncing ink-faint discs, kept from the previous
//     contract because the SSE state machine is off-limits for this pass.
//
// Public contract: { message: Message } — unchanged from the previous
// implementation, so every call-site (ChatWindow, scroll-into-view,
// data-message-id query selector for the highlight hook) keeps working.

import Markdown from 'react-markdown';
import { ToolCallsBlock } from './ToolCallsBlock';
import { ChannelMark } from '@/components/ui/channel-mark';
import type { Message } from '@/types/chat';

export function MessageBubble({ message }: { message: Message }) {
  const isUser = message.role === 'user';
  const isStreamingEmpty = message.status === 'streaming' && !message.content;

  if (isUser) {
    return (
      <div data-message-id={message.id} className="mb-5 flex justify-end">
        <div className="max-w-[78%] whitespace-pre-wrap text-right text-sm leading-relaxed text-ink">
          {message.content}
        </div>
      </div>
    );
  }

  return (
    <div data-message-id={message.id} className="mb-5 flex justify-start gap-3">
      <ChannelMark name="OneVoice" size={22} className="mt-1" />
      <div className="max-w-[78%] flex-1">
        <div className="rounded-md border border-line bg-paper-raised px-4 py-3 text-sm leading-relaxed text-ink shadow-ov-1">
          {isStreamingEmpty ? (
            <span className="flex gap-1" aria-label="OneVoice печатает">
              <span className="h-2 w-2 animate-bounce rounded-full bg-ink-faint [animation-delay:0ms]" />
              <span className="h-2 w-2 animate-bounce rounded-full bg-ink-faint [animation-delay:150ms]" />
              <span className="h-2 w-2 animate-bounce rounded-full bg-ink-faint [animation-delay:300ms]" />
            </span>
          ) : (
            <div className="prose prose-sm max-w-none prose-p:my-1 prose-ol:my-1 prose-ul:my-1 prose-li:my-0.5">
              <Markdown>{message.content}</Markdown>
            </div>
          )}
        </div>
        {message.toolCalls && message.toolCalls.length > 0 && (
          <ToolCallsBlock toolCalls={message.toolCalls} />
        )}
      </div>
    </div>
  );
}
