'use client';

import { useTranslations } from 'next-intl';

import { PlatformIcon } from './PlatformIcons';

import { ActionButton } from '@/components/design-system/ActionButton';
import { PermissionLoadError } from '@/components/permission/PermissionLoadError';
import { MonoLabel } from '@/components/ui/mono-label';
import { useChannelDemand, type RequestedChannel } from '@/hooks/useChannelDemand';
import { usePermission } from '@/lib/hooks/usePermission';

interface FuturePlatform {
  id: string;
  fullLabel: string;
  comingSoonWhen?: string;
}

interface ChannelDemandCardsProps {
  businessId: string | null;
  platforms: FuturePlatform[];
}

function requestedChannel(id: string): RequestedChannel | null {
  return id === 'avito' || id === '2gis' || id === 'wildberries' || id === 'ozon' ? id : null;
}

/** Future channels remain unavailable; demand capture only records organization interest. */
export function ChannelDemandCards({ businessId, platforms }: ChannelDemandCardsProps) {
  const t = useTranslations('integrations.demand');
  const tPlatforms = useTranslations('platforms');
  const readPermission = usePermission('content.read');
  const writePermission = usePermission('content.create');
  const demand = useChannelDemand(businessId, readPermission.allowed, writePermission.allowed);
  const cards = [
    ...platforms,
    ...(['wildberries', 'ozon'] as const)
      .filter((id) => !platforms.some((p) => p.id === id))
      .map((id) => ({ id, fullLabel: t(id), comingSoonWhen: tPlatforms('comingSoonWhen') })),
  ];

  return (
    <div className="space-y-4">
      <p className="text-sm text-ink-mid">{t('hint')}</p>
      {(readPermission.isError || writePermission.isError) && (
        <PermissionLoadError
          onRetry={readPermission.isError ? readPermission.refetch : writePermission.refetch}
        />
      )}
      {demand.isError && (
        <div role="alert" className="flex flex-wrap items-center gap-3 text-sm text-ink-mid">
          <span>{t('loadError')}</span>
          <ActionButton variant="secondary" size="sm" onClick={() => void demand.refetch()}>
            {t('retry')}
          </ActionButton>
        </div>
      )}
      <div className="grid grid-cols-1 items-start gap-4 md:grid-cols-2 lg:grid-cols-3">
        {cards.map((platform) => {
          const channel = requestedChannel(platform.id);
          const saved = demand.requested.has(platform.id);
          return (
            <div
              key={platform.id}
              className="flex flex-wrap items-center gap-4 rounded-lg border border-dashed border-line bg-paper-raised p-5"
            >
              <span
                aria-hidden
                className="flex h-10 w-10 shrink-0 items-center justify-center rounded-md border border-line-soft bg-paper-sunken"
              >
                <PlatformIcon platform={platform.id} className="h-6 w-6" />
              </span>
              <div className="min-w-32 flex-1">
                <div className="text-[15px] font-medium text-ink">{platform.fullLabel}</div>
                <MonoLabel className="mt-0.5">
                  {platform.comingSoonWhen ?? tPlatforms('comingSoonFallback')}
                </MonoLabel>
              </div>
              {channel && (writePermission.allowed || saved) && (
                <ActionButton
                  className="ml-auto"
                  variant="ghost"
                  size="sm"
                  disabled={
                    saved || !demand.isReady || demand.isPending || !writePermission.allowed
                  }
                  aria-label={t(saved ? 'savedLabel' : 'requestLabel', {
                    platform: platform.fullLabel,
                  })}
                  onClick={() => demand.request(channel)}
                >
                  {t(saved ? 'saved' : 'request')}
                </ActionButton>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}
