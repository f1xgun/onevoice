'use client';

import { PlatformIcon } from '@/components/integrations/PlatformIcons';
import Link from 'next/link';
import { useTranslations } from 'next-intl';
import { Check } from 'lucide-react';
import type { OnboardingChannel } from '@/hooks/useOnboardingProgress';
import { useBusinessStore } from '@/lib/stores/business';
import { trackEvent } from '@/lib/telemetry';

interface OnboardingPlatformRowsProps {
  channels: OnboardingChannel[];
}

export function OnboardingPlatformRows({ channels }: OnboardingPlatformRowsProps) {
  const t = useTranslations('gettingStarted.channels');
  const businessId = useBusinessStore((s) => s.activeBusinessId);
  function handleConnect(channel: OnboardingChannel) {
    if (businessId)
      trackEvent('activation', 'activation_step', {
        metadata: { business_id: businessId, platform: channel.platform, step: 'connectChannel' },
      });
  }
  if (channels.length === 0) return null;
  return (
    <ul className="mt-1 flex w-full flex-col gap-1 pl-9" aria-label={t('title')}>
      {channels.map((channel) => (
        <li key={channel.platform} className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
          <span className="flex items-center gap-1.5 font-medium">
            {channel.state === 'connected' && (
              <Check aria-hidden className="h-4 w-4 text-success" />
            )}
            <PlatformIcon platform={channel.platform} className="h-4 w-4" />
            {channel.label}
          </span>
          <span className="text-xs text-muted-foreground">{t(`status.${channel.state}`)}</span>
          {channel.canConnect && (
            <Link
              href={channel.href}
              className="inline-flex min-h-11 items-center text-brand underline underline-offset-4"
              aria-label={`${channel.state === 'error' ? t('reconnect') : t('connect')} ${channel.label}`}
              onClick={() => handleConnect(channel)}
            >
              {channel.state === 'error' ? t('reconnect') : t('connect')}
            </Link>
          )}
        </li>
      ))}
    </ul>
  );
}
