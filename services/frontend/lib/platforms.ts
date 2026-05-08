// Single source of truth for platform UI metadata. The list of platforms
// (which ids exist, what their status is) is fetched from
// GET /api/v1/platforms via lib/hooks/usePlatforms — this file owns only the
// presentation side: colors, labels, icons, display order. The backend
// registry uses the same ids verbatim, so a join by id is exact.
//
// Adding a new platform = one entry in PLATFORM_META + one entry in the Go
// registry (pkg/domain/platform.go). All consumers (landing, /integrations,
// filter dropdowns, tool whitelist UI, chat tool cards) read from here, so
// nothing else needs to be touched.

export type PlatformId =
  | 'telegram'
  | 'vk'
  | 'yandex_business'
  | 'google_business'
  | '2gis'
  | 'avito'
  | 'whatsapp';

export interface PlatformMeta {
  color: string;
  shortLabel: string;
  fullLabel: string;
  displayOrder: number;
  // Optional Linen-design "soon" subtitle (e.g. "Q3 2026", "оценивается").
  // Pure presentation; the backend registry only knows status, not timing.
  comingSoonWhen?: string;
}

export const PLATFORM_META: Record<PlatformId, PlatformMeta> = {
  telegram: { color: '#2AABEE', shortLabel: 'TG', fullLabel: 'Telegram', displayOrder: 0 },
  vk: { color: '#4680C2', shortLabel: 'VK', fullLabel: 'ВКонтакте', displayOrder: 1 },
  yandex_business: {
    color: '#FC3F1D',
    shortLabel: 'YB',
    fullLabel: 'Яндекс.Бизнес',
    displayOrder: 2,
  },
  google_business: {
    color: '#1A73E8',
    shortLabel: 'GB',
    fullLabel: 'Google Business',
    displayOrder: 3,
    comingSoonWhen: 'оценивается',
  },
  '2gis': {
    color: '#1DA045',
    shortLabel: '2G',
    fullLabel: '2ГИС',
    displayOrder: 4,
    comingSoonWhen: 'оценивается',
  },
  avito: {
    color: '#00AAFF',
    shortLabel: 'AV',
    fullLabel: 'Авито',
    displayOrder: 5,
    comingSoonWhen: 'оценивается',
  },
  whatsapp: {
    color: '#25D366',
    shortLabel: 'WA',
    fullLabel: 'WhatsApp',
    displayOrder: 6,
    comingSoonWhen: 'оценивается',
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

export function getPlatform(toolName: string): string {
  return toolName.split('__')[0] ?? toolName;
}

export function isKnownPlatform(id: string): id is PlatformId {
  return id in PLATFORM_META;
}
