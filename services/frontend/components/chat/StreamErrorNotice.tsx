'use client';

// components/chat/StreamErrorNotice.tsx — localized inline notice for a
// stream-level chat error frame. Replaces the old `[Error: <raw>]` splice in
// lib/sse.ts: the headline is always a calm localized message resolved by the
// machine-readable code (chat.streamError.*). The raw orchestrator detail is
// surfaced only inside an expandable diagnostics affordance — never as the
// primary user-facing text. An unknown / missing code resolves to the generic
// localized fallback.

import { AlertTriangle } from 'lucide-react';
import { useTranslations } from 'next-intl';

import { chatErrorKey } from '@/lib/chatError';
import type { ChatErrorCode } from '@/types/chat';

export interface StreamErrorNoticeProps {
  code?: ChatErrorCode;
  detail?: string;
}

export function StreamErrorNotice({ code, detail }: StreamErrorNoticeProps) {
  const t = useTranslations('chat.streamError');
  const summary = t(chatErrorKey(code));

  return (
    <div
      role="alert"
      aria-live="polite"
      className="mt-2 flex items-start gap-2 rounded-md border border-line bg-paper-raised px-3 py-2 text-sm text-ink"
      style={{ borderLeftColor: 'var(--destructive)', borderLeftWidth: 3 }}
    >
      <AlertTriangle
        size={16}
        className="mt-0.5 shrink-0 text-[var(--ov-danger)]"
        aria-hidden="true"
      />
      <div className="min-w-0 flex-1">
        <p>{summary}</p>
        {detail && (
          <details className="mt-1">
            <summary className="cursor-pointer text-xs text-ink-soft">{t('detailsLabel')}</summary>
            <p className="mt-1 whitespace-pre-wrap break-words font-mono text-xs text-ink-soft">
              {detail}
            </p>
          </details>
        )}
      </div>
    </div>
  );
}
