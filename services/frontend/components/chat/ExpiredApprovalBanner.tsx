'use client';

import { useState } from 'react';
import { AlertTriangle, X } from 'lucide-react';
import { useTranslations } from 'next-intl';

import { cn } from '@/lib/utils';

export interface ExpiredApprovalBannerProps {
  /**
   * Optional callback invoked after the user dismisses the banner. The banner
   * self-manages its visibility via internal state — parents use this hook for
   * telemetry or to clear related UI state. Not persisted across page reloads;
   * the server-side TTL remains the source of truth (Phase 16 D-19).
   */
  onDismiss?: () => void;
}

export function ExpiredApprovalBanner({ onDismiss }: ExpiredApprovalBannerProps) {
  const t = useTranslations('chat.expiredBanner');
  const [visible, setVisible] = useState(true);

  if (!visible) {
    return null;
  }

  return (
    <div
      role="alert"
      aria-live="polite"
      className={cn(
        'flex items-start gap-3 border-b px-4 py-3 text-sm',
        'bg-warning-soft',
        'border-amber-200',
        'text-amber-900'
      )}
    >
      <AlertTriangle size={16} className="mt-0.5 shrink-0" aria-hidden="true" />
      <span className="flex-1">{t('message')}</span>
      <button
        type="button"
        aria-label={t('dismissLabel')}
        onClick={() => {
          setVisible(false);
          onDismiss?.();
        }}
        className="shrink-0 rounded p-1 hover:bg-warning-soft"
      >
        <X size={14} aria-hidden="true" />
      </button>
    </div>
  );
}
