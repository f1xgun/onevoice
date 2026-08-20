// app/(app)/posts/_helpers.ts — page-local pure helpers.
//
// Extracted from the original posts/page.tsx as part of
// (DataTable adoption). Keeping these in a sibling _helpers.ts (Next
// convention: `_`-prefix means "not a route") lets the page shell stay
// under the SC-01 LOC ceiling while preserving co-location.

import { format, type Locale as DateFnsLocale } from 'date-fns';

import type { PlatformResult, Post } from '@/types/post';

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

// Helper stays pure: caller hands in the active date-fns locale via
// `getDateFnsLocale(useLocale)` so this module never has to import a
// next-intl runtime hook (and the `_helpers.ts` server-helper
// invariant is preserved).
export function nextScheduledLabel(posts: Post[], locale: DateFnsLocale): string {
  const upcoming = posts
    .filter((p) => p.status === 'scheduled' && p.scheduledAt)
    .map((p) => new Date(p.scheduledAt as string))
    .sort((a, b) => a.getTime() - b.getTime());
  if (upcoming.length === 0) return '—';
  return format(upcoming[0], 'd MMM', { locale });
}

export function topLevelErrorStatus(post: Post): boolean {
  return post.status === 'error';
}

// One channel of a broadcast row: the platform id plus the per-channel
// delivery result of the member post that targeted it.
export interface BroadcastChannelResult {
  platform: string;
  result: PlatformResult;
}

// PostRow is what the posts table renders: either a plain Post or a merged
// broadcast row. broadcastChannels is present ONLY on merged rows (two or
// more posts sharing a broadcastGroupId) and carries every member's
// per-channel result, so a partial failure stays visible channel by channel.
export interface PostRow extends Post {
  broadcastChannels?: BroadcastChannelResult[];
}

export function isChannelError(result: PlatformResult): boolean {
  return Boolean(result.error) || result.status === 'error';
}

// mergeBroadcastGroups collapses the posts fanned out by one broadcast turn
// (same non-empty broadcastGroupId, 2+ members) into a single PostRow at the
// first member's position, keeping every other post untouched. Posts without
// a group id — every record created before the field existed — render exactly
// as before, so the history stays backward compatible.
export function mergeBroadcastGroups(posts: Post[]): PostRow[] {
  const groupSize = new Map<string, number>();
  for (const p of posts) {
    if (p.broadcastGroupId) {
      groupSize.set(p.broadcastGroupId, (groupSize.get(p.broadcastGroupId) ?? 0) + 1);
    }
  }

  const mergedByGroup = new Map<string, PostRow>();
  const rows: PostRow[] = [];
  for (const p of posts) {
    const gid = p.broadcastGroupId;
    if (!gid || (groupSize.get(gid) ?? 0) < 2) {
      rows.push(p);
      continue;
    }
    let row = mergedByGroup.get(gid);
    if (!row) {
      row = { ...p, id: `broadcast-${gid}`, platformResults: {}, broadcastChannels: [] };
      mergedByGroup.set(gid, row);
      rows.push(row);
    }
    appendBroadcastMember(row, p);
  }
  for (const row of mergedByGroup.values()) {
    row.status = broadcastStatus(row.broadcastChannels ?? []);
  }
  return rows;
}

// appendBroadcastMember folds one member post into its merged broadcast row:
// its per-channel results join broadcastChannels verbatim, while the
// platformResults map (which feeds the platform chips and the open-link
// helper) keeps at most one entry per platform, preferring the errored one so
// a failure is never masked by a duplicate success.
function appendBroadcastMember(row: PostRow, member: Post): void {
  const results = member.platformResults ?? {};
  for (const [platform, result] of Object.entries(results)) {
    row.broadcastChannels?.push({ platform, result });
    const existing = row.platformResults?.[platform];
    if (
      row.platformResults &&
      (!existing || (!isChannelError(existing) && isChannelError(result)))
    ) {
      row.platformResults[platform] = result;
    }
  }
  if (member.mediaUrls?.length) {
    const seen = new Set(row.mediaUrls ?? []);
    row.mediaUrls = [...(row.mediaUrls ?? []), ...member.mediaUrls.filter((u) => !seen.has(u))];
  }
}

// broadcastStatus derives the merged row's status from its channels: every
// channel failed → error, some failed → partial (rendered as "M из N"), none
// failed → published.
function broadcastStatus(channels: BroadcastChannelResult[]): string {
  const failed = channels.filter((c) => isChannelError(c.result)).length;
  if (failed === 0) return 'published';
  if (failed === channels.length) return 'error';
  return 'partial';
}
