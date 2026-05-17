// app/(app)/posts/_helpers.ts — page-local pure helpers.
//
// Extracted from the original posts/page.tsx as part of Phase 19 / 19-12
// (DataTable adoption). Keeping these in a sibling _helpers.ts (Next
// convention: `_`-prefix means "not a route") lets the page shell stay
// under the SC-01 LOC ceiling while preserving co-location.

import { format } from 'date-fns';
import { ru } from 'date-fns/locale';

import type { Post } from '@/types/post';

// Backend platform ids that have a user-facing short label under
// posts.platformShort.<id>. Consumers (ChannelChip, PlatformResultCard)
// resolve the actual string via `useTranslations('posts.platformShort')`
// and fall back to the raw id outside this set.
export const PLATFORM_SHORT_KEYS: ReadonlySet<string> = new Set([
  'telegram',
  'vk',
  'yandex_business',
]);

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

export function topLevelErrorStatus(post: Post): boolean {
  // Pure boolean — i18n-aware consumers render the fallback string from
  // posts.errorFallback when this returns true.
  return post.status === 'error';
}
