// components/chat/MessageBubble.tsx — OneVoice (Linen) chat message
//
// Design contract from design_handoff_onevoice 2/mocks/mock-ai-chat.jsx:
//   - User messages: right-aligned ink-fill bubble (dark graphite bg,
//     paper text), rounded with a smaller top-right corner — the
//     conventional chat "tail" trick.
//   - Assistant messages: left-aligned, prefixed with the OneVoice
//     ChannelMark, body on a quiet paper-raised card with a 1 px line
//     border. Markdown rendered inside.
//   - Streaming dots: bouncing ink-faint discs alongside a visible
//     localized caption (UI-SPEC: operators need to read the state, not
//     just see animated dots).
//
// Public contract: { message: Message } — unchanged from the previous
// implementation, so every call-site (ChatWindow, scroll-into-view,
// data-message-id query selector for the highlight hook) keeps working.

import Markdown from 'react-markdown';
import { useTranslations } from 'next-intl';
import { ToolCallsBlock } from './ToolCallsBlock';
import { ChannelMark } from '@/components/ui/channel-mark';
import type { Message } from '@/types/chat';

export function MessageBubble({ message }: { message: Message }) {
  const tWindow = useTranslations('chat.window');
  const isUser = message.role === 'user';
  const hasContent = !!message.content;
  const hasToolCalls = (message.toolCalls?.length ?? 0) > 0;
  const isStreamingEmpty = message.status === 'streaming' && !hasContent;
  const isDoneEmpty = message.status === 'done' && !hasContent;

  if (isUser) {
    return (
      <div data-message-id={message.id} className="mb-5 flex justify-end">
        <div className="max-w-[78%] whitespace-pre-wrap rounded-md rounded-tr-[4px] bg-ink px-4 py-3 text-sm leading-relaxed text-paper">
          {message.content}
        </div>
      </div>
    );
  }

  if (isDoneEmpty && !hasToolCalls) {
    return null;
  }

  return (
    <div data-message-id={message.id} className="mb-5 flex justify-start gap-3">
      <ChannelMark name="OneVoice" size={22} className="mt-1" />
      <div className="max-w-[78%] flex-1">
        {!isDoneEmpty && (
          <div className="rounded-md border border-line bg-paper-raised px-4 py-3 text-sm leading-relaxed text-ink shadow-ov-1">
            {isStreamingEmpty ? (
              <span
                className="flex items-center gap-2 text-ink-mid"
                aria-label={tWindow('typingAria')}
              >
                <span className="flex gap-1" aria-hidden="true">
                  <span className="h-2 w-2 animate-bounce rounded-full bg-ink-faint [animation-delay:0ms]" />
                  <span className="h-2 w-2 animate-bounce rounded-full bg-ink-faint [animation-delay:150ms]" />
                  <span className="h-2 w-2 animate-bounce rounded-full bg-ink-faint [animation-delay:300ms]" />
                </span>
                <span className="text-xs">{tWindow('typingAria')}</span>
              </span>
            ) : (
              <div className="prose prose-sm max-w-none prose-p:my-1 prose-ol:my-1 prose-ul:my-1 prose-li:my-0.5">
                <Markdown>{message.content}</Markdown>
              </div>
            )}
          </div>
        )}
        {hasToolCalls && <ToolCallsBlock toolCalls={message.toolCalls!} />}
      </div>
    </div>
  );
}
