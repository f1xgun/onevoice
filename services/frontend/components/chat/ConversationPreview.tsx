'use client';

import { useEffect, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { useBusinessStore } from '@/lib/stores/business';

const PREVIEW_MAX_LENGTH = 160;

interface PreviewMessage {
  role: string;
  content?: string;
}

export function conversationPreview(messages: PreviewMessage[]): string {
  const message = [...messages]
    .reverse()
    .find((item) => (item.role === 'user' || item.role === 'assistant') && item.content?.trim());
  const text = message?.content?.replace(/\s+/g, ' ').trim() ?? '';
  return text.length > PREVIEW_MAX_LENGTH
    ? `${text.slice(0, PREVIEW_MAX_LENGTH).trimEnd()}…`
    : text;
}

interface ConversationPreviewProps {
  conversationId: string;
}

export function ConversationPreview({ conversationId }: ConversationPreviewProps) {
  const ref = useRef<HTMLSpanElement>(null);
  const [visible, setVisible] = useState(false);

  useEffect(() => {
    if (typeof IntersectionObserver === 'undefined') return;
    const observer = new IntersectionObserver(([entry]) => {
      if (entry.isIntersecting) {
        setVisible(true);
        observer.disconnect();
      }
    });
    if (ref.current) observer.observe(ref.current);
    return () => observer.disconnect();
  }, []);

  return (
    <span ref={ref} className="block min-h-5 whitespace-normal break-words text-meta text-ink-soft">
      {visible && <PreviewText conversationId={conversationId} />}
    </span>
  );
}

function PreviewText({ conversationId }: ConversationPreviewProps) {
  const businessId = useBusinessStore((state) => state.activeBusinessId);
  const t = useTranslations('chat.rowMenu');
  const query = useQuery({
    queryKey: ['businesses', businessId, 'conversations', conversationId, 'preview'],
    queryFn: async ({ signal }) => {
      const { data } = await bizApi(businessId!).get<{ messages: PreviewMessage[] }>(
        BIZ_API_PATHS.CONVERSATIONS.MESSAGES(conversationId),
        { signal }
      );
      return conversationPreview(data.messages);
    },
    enabled: !!businessId,
    staleTime: 30_000,
    retry: false,
  });
  if (query.isError) return t('previewUnavailable');
  if (!query.isSuccess) return null;
  return query.data || t('noMessages');
}
