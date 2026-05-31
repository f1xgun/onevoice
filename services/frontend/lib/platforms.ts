// Single source of truth for platform UI metadata. The list of platforms
// (which ids exist, what their status is) is fetched from
// GET /api/v1/platforms via lib/hooks/usePlatforms — this file owns only the
// presentation side: colors, labels, icons, display order. The backend
// registry uses the same ids verbatim, so a join by id is exact.
//
// Adding a new platform = one entry in PLATFORM_STATIC_META + one entry in
// the Go registry (pkg/domain/platform.go) + one entry under
// `platforms.fullLabel` in every locale bundle in messages/*.json. All
// consumers (landing, /integrations, filter dropdowns, tool whitelist UI,
// chat tool cards) read from here, so nothing else needs to be touched.
//
// Anything that depends on a string from messages/*.json lives behind a
// factory (`createPlatformMeta(t)` / `createPlatformFullLabels(t)`).
// Static fields ship to the client bundle as-is via PLATFORM_STATIC_META.
// React consumers get the merged shape via the `usePlatformMeta` hook.

import { useMemo } from 'react';
import { useTranslations } from 'next-intl';

// Translator shapes we depend on. Declared structurally so this module
// doesn't have to import next-intl just to type the factory.
type PlatformsTranslator = (key: 'comingSoonWhen') => string;
type FullLabelTranslator = (key: PlatformId) => string;

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

// Locale-invariant slice: color, shortLabel ("TG", "VK"), displayOrder,
// defaultStatus. These never change per language and stay in module scope
// so existing static consumers (parity tests, fallback enrichment in
// usePlatforms) keep their cheap lookup.
export interface PlatformStaticMeta {
  color: string;
  shortLabel: string;
  displayOrder: number;
  defaultStatus: PlatformDefaultStatus;
}

export const PLATFORM_STATIC_META: Record<PlatformId, PlatformStaticMeta> = {
  telegram: { color: '#2AABEE', shortLabel: 'TG', displayOrder: 0, defaultStatus: 'active' },
  vk: { color: '#4680C2', shortLabel: 'VK', displayOrder: 1, defaultStatus: 'active' },
  yandex_business: {
    color: '#FC3F1D',
    shortLabel: 'YB',
    displayOrder: 2,
    defaultStatus: 'active',
  },
  google_business: {
    color: '#1A73E8',
    shortLabel: 'GB',
    displayOrder: 3,
    defaultStatus: 'coming_soon',
  },
  '2gis': { color: '#1DA045', shortLabel: '2G', displayOrder: 4, defaultStatus: 'coming_soon' },
  avito: { color: '#00AAFF', shortLabel: 'AV', displayOrder: 5, defaultStatus: 'coming_soon' },
  whatsapp: { color: '#25D366', shortLabel: 'WA', displayOrder: 6, defaultStatus: 'coming_soon' },
};

export const PLATFORM_DISPLAY_ORDER: PlatformId[] = (
  Object.keys(PLATFORM_STATIC_META) as PlatformId[]
).sort((a, b) => PLATFORM_STATIC_META[a].displayOrder - PLATFORM_STATIC_META[b].displayOrder);

// Locale-invariant view-objects. Existing call sites read these with
// arbitrary string keys (platform ids parsed out of tool names like
// "telegram__send"). Re-derived from PLATFORM_STATIC_META so adding a
// platform stays single-source.
export const PLATFORM_COLORS: Record<string, string> = Object.fromEntries(
  (Object.keys(PLATFORM_STATIC_META) as PlatformId[]).map((id) => [
    id,
    PLATFORM_STATIC_META[id].color,
  ])
);

export const PLATFORM_LABELS: Record<string, string> = Object.fromEntries(
  (Object.keys(PLATFORM_STATIC_META) as PlatformId[]).map((id) => [
    id,
    PLATFORM_STATIC_META[id].shortLabel,
  ])
);

// Request-scoped factories. Use the hook from React components; call the
// factory directly from server-side code with a resolved translator.

export function createPlatformFullLabels(t: FullLabelTranslator): Record<string, string> {
  return Object.fromEntries(
    (Object.keys(PLATFORM_STATIC_META) as PlatformId[]).map((id) => [id, t(id)])
  );
}

export function createPlatformMeta(
  tFullLabel: FullLabelTranslator,
  tPlatforms: PlatformsTranslator
): Record<PlatformId, PlatformMeta> {
  const comingSoonWhen = tPlatforms('comingSoonWhen');
  const out = {} as Record<PlatformId, PlatformMeta>;
  for (const id of Object.keys(PLATFORM_STATIC_META) as PlatformId[]) {
    const base = PLATFORM_STATIC_META[id];
    out[id] = {
      color: base.color,
      shortLabel: base.shortLabel,
      displayOrder: base.displayOrder,
      defaultStatus: base.defaultStatus,
      fullLabel: tFullLabel(id),
      ...(base.defaultStatus === 'coming_soon' ? { comingSoonWhen } : {}),
    };
  }
  return out;
}

// React-tree hooks — the canonical consumer surface for B1. Memoize on
// the translator identity so callers can safely pass the records into
// dependency arrays.
export function usePlatformMeta(): Record<PlatformId, PlatformMeta> {
  const tFullLabel = useTranslations('platforms.fullLabel') as FullLabelTranslator;
  const tPlatforms = useTranslations('platforms') as PlatformsTranslator;
  return useMemo(() => createPlatformMeta(tFullLabel, tPlatforms), [tFullLabel, tPlatforms]);
}

export function usePlatformFullLabels(): Record<string, string> {
  const tFullLabel = useTranslations('platforms.fullLabel') as FullLabelTranslator;
  return useMemo(() => createPlatformFullLabels(tFullLabel), [tFullLabel]);
}

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
  return id in PLATFORM_STATIC_META;
}

// Two-letter mono initials for the platform mark on integration cards.
// Distinct from `shortLabel` (Latin codes used elsewhere) because the
// integrations UI ships localized initials (e.g. Cyrillic 'ЯБ' for
// yandex_business). Record<PlatformId, …> forces a new platform to declare
// its initials at compile time.
export const PLATFORM_INITIALS: Record<PlatformId, string> = {
  telegram: 'TG',
  vk: 'VK',
  yandex_business: 'ЯБ',
  google_business: 'GB',
  '2gis': '2G',
  avito: 'AV',
  whatsapp: 'WA',
};

export function platformInitials(platform: string, fallbackLabel: string): string {
  if (isKnownPlatform(platform)) return PLATFORM_INITIALS[platform];
  return fallbackLabel.slice(0, 2).toUpperCase();
}

// Platform-specific metadata-field name carried on Integration documents.
// Wired by both `getIntegrationDisplay` (read) and the lazy-refresh button
// in PlatformCard (write — knows which endpoint to call). Keeping the
// mapping here means adding a new platform with its own display field is
// one entry instead of two switch arms.
export const PLATFORM_DISPLAY_FIELD: Partial<Record<PlatformId, string>> = {
  telegram: 'channel_title',
  vk: 'community_name',
  yandex_business: 'business_name',
};

// Reconnect CTA i18n key per platform. Single source of truth shared by
// the chat IntegrationTokenInvalidBanner and the tasks-page explainError
// so the two surfaces can't drift on label copy. Adding a new PlatformId
// forces a key here at compile time (Record<PlatformId, …>); platforms
// without a dedicated label fall back to the generic 'reconnect' key.
export const PLATFORM_RECONNECT_LABEL_KEYS: Record<PlatformId, string> = {
  telegram: 'reconnectTelegram',
  vk: 'reconnectVk',
  yandex_business: 'reconnectYandex',
  google_business: 'reconnectGoogle',
  '2gis': 'reconnect',
  avito: 'reconnect',
  whatsapp: 'reconnect',
};

export function reconnectLabelKey(platform: string | null | undefined): string {
  if (!platform || !isKnownPlatform(platform)) return 'reconnect';
  return PLATFORM_RECONNECT_LABEL_KEYS[platform];
}

// Token-error summary i18n key per platform. Mirrors the
// PLATFORM_RECONNECT_LABEL_KEYS pattern so the chat banner and the tasks
// explainError can't drift on the user-facing "token invalid" copy.
// Platforms without dedicated copy share the generic 'tokenGeneric' key
// (resolved via tokenErrorKey() for unknown / null inputs as well).
export const PLATFORM_TOKEN_ERROR_KEYS: Record<PlatformId, string> = {
  telegram: 'tokenTelegram',
  vk: 'tokenVk',
  yandex_business: 'tokenGeneric',
  google_business: 'tokenGeneric',
  '2gis': 'tokenGeneric',
  avito: 'tokenGeneric',
  whatsapp: 'tokenGeneric',
};

export function tokenErrorKey(platform: string | null | undefined): string {
  if (!platform || !isKnownPlatform(platform)) return 'tokenGeneric';
  return PLATFORM_TOKEN_ERROR_KEYS[platform];
}

// Lazy-refresh endpoints used by PlatformCard to backfill missing
// friendly names. Keys are the PlatformIds whose agents expose a
// refresh-name endpoint; values are functions producing the path
// (relative to /businesses/{id}, matching BIZ_API_PATHS conventions).
// Platforms not in this map skip the lazy backfill.
export const PLATFORM_REFRESH_ENDPOINTS: Partial<
  Record<PlatformId, (integrationId: string) => string>
> = {
  vk: (id) => `/integrations/vk/${id}/refresh-name`,
  yandex_business: (id) => `/integrations/yandex_business/${id}/refresh-name`,
};
