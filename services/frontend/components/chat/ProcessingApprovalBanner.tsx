'use client';

import { Loader2 } from 'lucide-react';
import { useTranslations } from 'next-intl';

import { cn } from '@/lib/utils';

/**
 * ProcessingApprovalBanner renders while a batch is in `resolving` status — the
 * user already approved and the resume is executing server-side. It replaces the
 * actionable ToolApprovalCard so a page reload mid-resume cannot re-submit the
 * batch (which would 409 "already_resolved"). Non-interactive by design; the
 * banner clears on the next load once the batch reaches a terminal state and
 * drops out of the conversation's pending approvals.
 */
export function ProcessingApprovalBanner() {
  const t = useTranslations('chat.processingBanner');

  return (
    <div
      role="status"
      aria-live="polite"
      className={cn(
        'flex items-start gap-3 border-b px-4 py-3 text-sm',
        'bg-muted/50',
        'text-muted-foreground'
      )}
    >
      <Loader2 size={16} className="mt-0.5 shrink-0 animate-spin" aria-hidden="true" />
      <span className="flex-1">{t('message')}</span>
    </div>
  );
}
