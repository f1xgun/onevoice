'use client';

import { useTranslations } from 'next-intl';

interface ConversationPreviewProps {
  preview?: string;
}

export function ConversationPreview({ preview }: ConversationPreviewProps) {
  const t = useTranslations('chat.rowMenu');
  return (
    <span className="block min-h-5 whitespace-normal break-words text-meta text-ink-soft">
      {preview === undefined ? t('previewUnavailable') : preview || t('noMessages')}
    </span>
  );
}
