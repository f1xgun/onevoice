'use client';

import { AlertTriangle } from 'lucide-react';
import Link from 'next/link';
import { useTranslations } from 'next-intl';

import { API_PATHS } from '@/lib/constants/apiPaths';
import { reconnectLabelKey } from '@/lib/platforms';
import { cn } from '@/lib/utils';

export interface IntegrationTokenInvalidBannerProps {
  /**
   * Platform slug derived from the failing toolCall name
   * (e.g. `telegram__send_channel_post` → `telegram`). Drives both the
   * summary copy variant and the reconnect deep-link.
   */
  platform: string;
}

export function IntegrationTokenInvalidBanner({ platform }: IntegrationTokenInvalidBannerProps) {
  const t = useTranslations('tasks.errors');

  const summaryKey =
    platform === 'telegram' ? 'tokenTelegram' : platform === 'vk' ? 'tokenVk' : 'tokenGeneric';

  const ctaKey = reconnectLabelKey(platform);
  const href = `${API_PATHS.INTEGRATIONS.ROOT}?reconnect=${platform}`;

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
      <div className="flex-1">
        <p>{t(summaryKey)}</p>
        <Link
          href={href}
          className="mt-2 inline-block rounded-md bg-amber-600 px-3 py-1 text-paper hover:bg-amber-700"
        >
          {t(ctaKey)}
        </Link>
      </div>
    </div>
  );
}
