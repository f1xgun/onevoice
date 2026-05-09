// app/(app)/posts/_helpers.ts — page-local pure helpers.
//
// Extracted from the original posts/page.tsx as part of Phase 19 / 19-12
// (DataTable adoption). Keeping these in a sibling _helpers.ts (Next
// convention: `_`-prefix means "not a route") lets the page shell stay
// under the SC-01 LOC ceiling while preserving co-location.

import { format } from 'date-fns';
import { ru } from 'date-fns/locale';

import type { Post } from '@/types/post';

// Backend platform id → user-facing short label. Used by ChannelChip and
// PlatformResultCard. The full names live in @/lib/platforms (CHANNEL_NAMES);
// the abbreviation here is for in-line chips that would otherwise crowd
// the row layout (e.g. yandex_business → "Яндекс").
export const platformShort: Record<string, string> = {
  telegram: 'Telegram',
  vk: 'VK',
  yandex_business: 'Яндекс',
};

export function collectPlatforms(post: Post): string[] {
  if (post.platformResults) {
    const keys = Object.keys(post.platformResults);
    if (keys.length > 0) return keys;
  }
  return [];
}

export function firstLink(post: Post): string | null {
  if (!post.platformResults) return null;
  for (const r of Object.values(post.platformResults)) {
    if (r.url) return r.url;
  }
  return null;
}

export function nextScheduledLabel(posts: Post[]): string {
  const upcoming = posts
    .filter((p) => p.status === 'scheduled' && p.scheduledAt)
    .map((p) => new Date(p.scheduledAt as string))
    .sort((a, b) => a.getTime() - b.getTime());
  if (upcoming.length === 0) return '—';
  return format(upcoming[0], 'd MMM', { locale: ru });
}

export function friendlyTopLevelError(post: Post): string | null {
  if (post.status !== 'error') return null;
  // Backend doesn't currently return a top-level error string, so we offer a
  // plain-Russian fallback that points the user at the next step.
  return 'Не удалось опубликовать. Проверьте подключение каналов и попробуйте ещё раз.';
}
