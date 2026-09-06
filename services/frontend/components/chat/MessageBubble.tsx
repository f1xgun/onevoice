import Markdown, { type Components } from 'react-markdown';
import { useMemo } from 'react';
import { useTranslations } from 'next-intl';
import { ToolCallsBlock } from './ToolCallsBlock';
import { TypingIndicator } from './TypingIndicator';
import { StreamErrorNotice } from './StreamErrorNotice';
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
      <div data-message-id={message.id} className="mx-auto mb-6 w-full max-w-[66ch]">
        <div className="min-w-0 whitespace-pre-wrap break-words rounded-md bg-paper-sunken px-4 py-3 text-reading text-ink">
          <p className="mb-2 text-meta font-medium">{tWindow('authorOwner')}</p>
          {message.content}
        </div>
      </div>
    );
  }

  if (isDoneEmpty && !hasToolCalls) {
    return null;
  }

  return (
    <div data-message-id={message.id} className="mx-auto mb-6 w-full min-w-0 max-w-[66ch]">
      <p className="mb-2 text-meta font-medium">{tWindow('authorAssistant')}</p>
      <div className="min-w-0">
        {!isDoneEmpty && (
          <div className="min-w-0 break-words text-reading text-ink">
            {isStreamingEmpty ? (
              <TypingIndicator label={tWindow('typingAria')} />
            ) : (
              <div className="prose max-w-none text-reading text-ink prose-headings:text-ink prose-p:my-1 prose-a:text-brand prose-blockquote:text-ink-soft prose-strong:text-ink prose-code:text-ink prose-pre:bg-paper-sunken prose-pre:text-ink prose-ol:my-1 prose-ul:my-1 prose-li:my-0.5">
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
