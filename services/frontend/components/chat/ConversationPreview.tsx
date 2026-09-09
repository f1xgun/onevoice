'use client';

import type { ReactNode } from 'react';
import Markdown, { type Components } from 'react-markdown';
import { useTranslations } from 'next-intl';

interface ConversationPreviewProps {
  preview?: string;
}

function InlineBlock({ children }: { children?: ReactNode }) {
  return <span>{children} </span>;
}

// Previews live inside the chat link/button: never nest links or fetch images.
// Keep inline emphasis, flatten block layout and ignore untrusted HTML.
const previewComponents: Components = {
  p: InlineBlock,
  h1: InlineBlock,
  h2: InlineBlock,
  h3: InlineBlock,
  h4: InlineBlock,
  h5: InlineBlock,
  h6: InlineBlock,
  ul: InlineBlock,
  ol: InlineBlock,
  li: InlineBlock,
  blockquote: InlineBlock,
  pre: InlineBlock,
  a: InlineBlock,
  img: ({ alt }) => <span>{alt}</span>,
  hr: () => <span> </span>,
  br: () => <span> </span>,
};

export function ConversationPreview({ preview }: ConversationPreviewProps) {
  const t = useTranslations('chat.rowMenu');
  return (
    <span className="line-clamp-2 min-h-5 whitespace-normal break-words text-meta text-ink-soft">
      {preview ? (
        <Markdown skipHtml components={previewComponents}>
          {preview}
        </Markdown>
      ) : preview === undefined ? (
        t('previewUnavailable')
      ) : (
        t('noMessages')
      )}
    </span>
  );
}
