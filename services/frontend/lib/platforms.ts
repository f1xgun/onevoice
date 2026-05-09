// Single source of truth for platform UI metadata. The list of platforms
// (which ids exist, what their status is) is fetched from
// GET /api/v1/platforms via lib/hooks/usePlatforms — this file owns only the
// presentation side: colors, labels, icons, display order. The backend
// registry uses the same ids verbatim, so a join by id is exact.
//
// Adding a new platform = one entry in PLATFORM_META + one entry in the Go
// registry (pkg/domain/platform.go) + one entry under platforms.fullLabel
// in messages/ru.json. All consumers (landing, /integrations, filter
// dropdowns, tool whitelist UI, chat tool cards) read from here, so
// nothing else needs to be touched.

import { getTranslator } from '@/lib/i18n/translator';

const tPlatforms = getTranslator('platforms');
const tFullLabel = getTranslator('platforms.fullLabel');

export type PlatformId =
  | 'telegram'
  | 'vk'
  | 'yandex_business'
  | 'google_business'
  | '2gis'
  | 'avito'
  | 'whatsapp';

// PlatformDefaultStatus mirrors the registry's at-rest state in
// pkg/domain/platform.go. The wire status returned by /api/v1/platforms is
// authoritative once it loads — but until the request resolves (and on
// network failure) the hook needs to fall back to something that won't
// advertise broken connect flows for non-MVP platforms. Keeping the
// pre-network status here, beside the rest of the metadata, is checked for
// drift by lib/__tests__/platforms.test.ts.
export type PlatformDefaultStatus = 'active' | 'coming_soon';

export interface PlatformMeta {
  color: string;
  shortLabel: string;
  fullLabel: string;
  displayOrder: number;
  defaultStatus: PlatformDefaultStatus;
  // Optional Linen-design "soon" subtitle (e.g. "Q3 2026", "оценивается").
  // Pure presentation; the backend registry only knows status, not timing.
  comingSoonWhen?: string;
}

const COMING_SOON_WHEN = tPlatforms('comingSoonWhen');

export const PLATFORM_META: Record<PlatformId, PlatformMeta> = {
  telegram: {
    color: '#2AABEE',
    shortLabel: 'TG',
    fullLabel: tFullLabel('telegram'),
    displayOrder: 0,
    defaultStatus: 'active',
  },
  vk: {
    color: '#4680C2',
    shortLabel: 'VK',
    fullLabel: tFullLabel('vk'),
    displayOrder: 1,
    defaultStatus: 'active',
  },
  yandex_business: {
    color: '#FC3F1D',
    shortLabel: 'YB',
    fullLabel: tFullLabel('yandex_business'),
    displayOrder: 2,
    defaultStatus: 'active',
  },
  google_business: {
    color: '#1A73E8',
    shortLabel: 'GB',
    fullLabel: tFullLabel('google_business'),
    displayOrder: 3,
    defaultStatus: 'coming_soon',
    comingSoonWhen: COMING_SOON_WHEN,
  },
  '2gis': {
    color: '#1DA045',
    shortLabel: '2G',
    fullLabel: tFullLabel('2gis'),
    displayOrder: 4,
    defaultStatus: 'coming_soon',
    comingSoonWhen: COMING_SOON_WHEN,
  },
  avito: {
    color: '#00AAFF',
    shortLabel: 'AV',
    fullLabel: tFullLabel('avito'),
    displayOrder: 5,
    defaultStatus: 'coming_soon',
    comingSoonWhen: COMING_SOON_WHEN,
  },
  whatsapp: {
    color: '#25D366',
    shortLabel: 'WA',
    fullLabel: tFullLabel('whatsapp'),
    displayOrder: 6,
    defaultStatus: 'coming_soon',
    comingSoonWhen: COMING_SOON_WHEN,
  },
};

export const PLATFORM_DISPLAY_ORDER: PlatformId[] = (
  Object.keys(PLATFORM_META) as PlatformId[]
).sort((a, b) => PLATFORM_META[a].displayOrder - PLATFORM_META[b].displayOrder);

// Legacy view-objects: existing call sites read these maps with arbitrary
// string keys (platform ids parsed out of tool names like "telegram__send").
// Re-deriving them from PLATFORM_META keeps a single source of truth without
// forcing a sweeping rename across components in this PR.
export const PLATFORM_COLORS: Record<string, string> = Object.fromEntries(
  (Object.keys(PLATFORM_META) as PlatformId[]).map((id) => [id, PLATFORM_META[id].color])
);

export const PLATFORM_LABELS: Record<string, string> = Object.fromEntries(
  (Object.keys(PLATFORM_META) as PlatformId[]).map((id) => [id, PLATFORM_META[id].shortLabel])
);

export const PLATFORM_FULL_LABELS: Record<string, string> = Object.fromEntries(
  (Object.keys(PLATFORM_META) as PlatformId[]).map((id) => [id, PLATFORM_META[id].fullLabel])
);

// Backend platform id → ChannelMark `name` prop. Latin technical names
// (Telegram/VK/Yandex.Business/Google/2GIS) — these key into the
// channelColor map inside <ChannelMark>. Distinct from PLATFORM_FULL_LABELS
// (cyrillic display labels): ChannelMark expects English brand names so
// the same primitive can render across locales.
export const CHANNEL_NAMES: Record<PlatformId, string> = {
  telegram: 'Telegram',
  vk: 'VK',
  yandex_business: 'Yandex.Business',
  google_business: 'Google',
  '2gis': '2GIS',
  avito: 'Avito',
  whatsapp: 'WhatsApp',
};

export function getPlatform(toolName: string): string {
  return toolName.split('__')[0] ?? toolName;
}

export function isKnownPlatform(id: string): id is PlatformId {
  return id in PLATFORM_META;
}
