'use client';

import { Badge } from '@/components/ui/badge';
import { ChannelMark } from '@/components/ui/channel-mark';
import { MonoLabel } from '@/components/ui/mono-label';
import { usePlatforms } from '@/lib/hooks/usePlatforms';
import type { PlatformStatus } from '@/lib/api/platforms';

// Landing-channels grid for the Linen homepage. The 7 entries with a
// registry id pull their "есть"/"скоро" status live from
// GET /api/v1/platforms (so adding a new active platform to pkg/domain
// automatically promotes its card here). The two editorial-only teasers
// at the end (Instagram, Одноклассники) carry status='скоро' inline since
// they have no agent and no roadmap commitment behind them.
//
// `name` matches a ChannelMark icon hint, `display` is the user-visible
// label, `meta` is the small Russian descriptor.
type LandingPlatform = {
  id: string | null;
  name: string;
  display: string;
  meta: string;
};

const LANDING_PLATFORMS: LandingPlatform[] = [
  { id: 'telegram', name: 'Telegram', display: 'Telegram', meta: 'Каналы и боты' },
  { id: 'vk', name: 'VK', display: 'ВКонтакте', meta: 'Сообщества' },
  { id: 'yandex_business', name: 'Yandex', display: 'Яндекс.Бизнес', meta: 'Карта и отзывы' },
  { id: 'google_business', name: 'Google', display: 'Google Business', meta: 'Оценивается' },
  { id: '2gis', name: '2GIS', display: '2ГИС', meta: 'Оценивается' },
  { id: 'avito', name: 'Avito', display: 'Авито', meta: 'Оценивается' },
  { id: 'whatsapp', name: 'WhatsApp', display: 'WhatsApp', meta: 'Оценивается' },
  // Editorial-only teasers — no backend registry entry, no agent. Stay
  // hardcoded so we can advertise interest without polluting the technical
  // platform list.
  { id: null, name: 'Instagram', display: 'Instagram', meta: 'Оценивается' },
  { id: null, name: 'OK', display: 'Одноклассники', meta: 'Оценивается' },
];

function statusForLanding(
  byId: Map<string, PlatformStatus>,
  id: string | null
): { tone: 'success' | 'neutral'; label: 'есть' | 'скоро' } {
  if (id === null) return { tone: 'neutral', label: 'скоро' };
  const status = byId.get(id);
  // Treat oauth_not_configured the same as coming_soon for the landing —
  // visitors don't care that an admin hasn't configured creds, they care
  // whether the channel is live for them.
  if (status === 'active') return { tone: 'success', label: 'есть' };
  return { tone: 'neutral', label: 'скоро' };
}

export function SupportedPlatforms() {
  const { platforms } = usePlatforms();
  const byId = new Map<string, PlatformStatus>(platforms.map((p) => [p.id, p.status]));

  return (
    <div className="mt-12 grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-5">
      {LANDING_PLATFORMS.map((p) => {
        const { tone, label } = statusForLanding(byId, p.id);
        return (
          <div
            key={p.display}
            className="flex min-h-[120px] flex-col gap-3 rounded-lg border border-line bg-paper-raised p-4"
          >
            <div className="flex items-center justify-between">
              <ChannelMark name={p.name} size={28} />
              <Badge tone={tone}>{label}</Badge>
            </div>
            <div className="mt-auto">
              <div className="text-[14px] font-medium">{p.display}</div>
              <MonoLabel>{p.meta}</MonoLabel>
            </div>
          </div>
        );
      })}
    </div>
  );
}
