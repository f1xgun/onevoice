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

import Markdown, { type Components } from 'react-markdown';
import { useMemo } from 'react';
import { useTranslations } from 'next-intl';
import { ToolCallsBlock } from './ToolCallsBlock';
import { TypingIndicator } from './TypingIndicator';
import { StreamErrorNotice } from './StreamErrorNotice';
import { ChannelMark } from '@/components/ui/channel-mark';
import { isTrustedImageSrc, safeExternalHref } from '@/lib/trustedImage';
import type { Message } from '@/types/chat';

export function MessageBubble({ message }: { message: Message }) {
  const tWindow = useTranslations('chat.window');
  const isUser = message.role === 'user';
  const hasContent = !!message.content;
  const hasToolCalls = (message.toolCalls?.length ?? 0) > 0;
  const hasError = !!message.errorCode || !!message.errorDetail;
  const isStreaming = message.status === 'streaming';
  const isStreamingEmpty = isStreaming && !hasContent;
  const isDoneEmpty = message.status === 'done' && !hasContent && !hasError;
  // Once tokens or a tool call have rendered, the empty-bubble dots are gone —
  // so without this footer the indicator would vanish during the wait for the
  // next LLM iteration and the operator can't tell OneVoice is still working.
  // Gated on hasContent so it never double-renders alongside the dots.
  const showWorkingFooter = isStreaming && hasContent;

  // Never auto-load an image the model emitted from an untrusted host: a
  // Markdown image in assistant output could be steered by injected review text
  // into a tracking/exfil fetch. First-party/same-origin images render inline;
  // anything else becomes a click-through link the operator opens deliberately.
  const markdownComponents = useMemo<Components>(
    () => ({
      img({ src, alt }) {
        const url = typeof src === 'string' ? src : '';
        const altText = typeof alt === 'string' ? alt : '';
        if (url && isTrustedImageSrc(url)) {
          // eslint-disable-next-line @next/next/no-img-element
          return <img src={url} alt={altText} className="max-w-full rounded-md" />;
        }
        const href = safeExternalHref(url);
        const label = altText.trim() !== '' ? altText : tWindow('externalImage');
        if (!href) {
          return <span className="text-ink-faint">{label}</span>;
        }
        return (
          <a
            href={href}
            target="_blank"
            rel="noopener noreferrer nofollow"
            className="underline decoration-dotted underline-offset-2"
          >
            {label}
          </a>
        );
      },
    }),
    [tWindow]
  );

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
              <TypingIndicator label={tWindow('typingAria')} />
            ) : (
              <div className="prose prose-sm max-w-none prose-p:my-1 prose-ol:my-1 prose-ul:my-1 prose-li:my-0.5">
                <Markdown components={markdownComponents}>{message.content}</Markdown>
              </div>
            )}
          </div>
        )}
        {hasToolCalls && <ToolCallsBlock toolCalls={message.toolCalls!} />}
        {hasError && <StreamErrorNotice code={message.errorCode} detail={message.errorDetail} />}
        {showWorkingFooter && (
          <TypingIndicator label={tWindow('typingAria')} className="mt-2 pl-1" />
        )}
      </div>
    </div>
  );
}
