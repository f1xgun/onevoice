'use client';

import { useMemo } from 'react';
import { useTranslations } from 'next-intl';

import { Badge } from '@/components/ui/badge';
import { ChannelMark } from '@/components/ui/channel-mark';
import { MonoLabel } from '@/components/ui/mono-label';
import { usePlatforms } from '@/lib/hooks/usePlatforms';
import type { PlatformStatus } from '@/lib/api/platforms';

// Landing-channels grid for the Linen homepage. The 7 entries with a
// registry id pull their live status from GET /api/v1/platforms (so
// adding a new active platform to pkg/domain automatically promotes
// its card here). The two editorial-only teasers at the end
// (Instagram, Odnoklassniki) have no agent / no roadmap commitment.
//
// `iconName` is an EN brand id (icon hint). The user-facing label + meta
// blurb resolve from i18n via landing.platforms.<key>.{display,meta}.
type LandingPlatform = {
  id: string | null;
  iconName: string;
  i18nKey: string;
};

const LANDING_PLATFORMS: LandingPlatform[] = [
  { id: 'telegram', iconName: 'Telegram', i18nKey: 'telegram' },
  { id: 'vk', iconName: 'VK', i18nKey: 'vk' },
  { id: 'yandex_business', iconName: 'Yandex', i18nKey: 'yandex_business' },
  { id: 'google_business', iconName: 'Google', i18nKey: 'google_business' },
  { id: '2gis', iconName: '2GIS', i18nKey: '2gis' },
  { id: 'avito', iconName: 'Avito', i18nKey: 'avito' },
  { id: 'whatsapp', iconName: 'WhatsApp', i18nKey: 'whatsapp' },
  // Editorial-only teasers — no backend registry entry, no agent.
  { id: null, iconName: 'Instagram', i18nKey: 'instagram' },
  { id: null, iconName: 'OK', i18nKey: 'ok' },
];

function statusForLanding(
  byId: Map<string, PlatformStatus>,
  id: string | null
): { tone: 'success' | 'neutral'; key: 'have' | 'soon' } {
  if (id === null) return { tone: 'neutral', key: 'soon' };
  const status = byId.get(id);
  if (status === 'active') return { tone: 'success', key: 'have' };
  return { tone: 'neutral', key: 'soon' };
}

export function SupportedPlatforms() {
  const { platforms } = usePlatforms();
  const tPlatforms = useTranslations('landing.platforms');
  const tStatus = useTranslations('landing.platformStatus');
  const byId = useMemo(
    () => new Map<string, PlatformStatus>(platforms.map((p) => [p.id, p.status])),
    [platforms]
  );

  return (
    <div className="mt-12 grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-5">
      {LANDING_PLATFORMS.map((p) => {
        const { tone, key } = statusForLanding(byId, p.id);
        const display = tPlatforms(`${p.i18nKey}.display`);
        const meta = tPlatforms(`${p.i18nKey}.meta`);
        return (
          <div
            key={display}
            className="flex min-h-[120px] flex-col gap-3 rounded-lg border border-line bg-paper-raised p-4"
          >
            <div className="flex items-center justify-between">
              <ChannelMark name={p.iconName} size={28} />
              <Badge tone={tone}>{tStatus(key)}</Badge>
            </div>
            <div className="mt-auto">
              <div className="text-[14px] font-medium">{display}</div>
              <MonoLabel>{meta}</MonoLabel>
            </div>
          </div>
        );
      })}
    </div>
  );
}
