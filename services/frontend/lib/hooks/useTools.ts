'use client';

import { useQuery } from '@tanstack/react-query';
import { fetchTools } from '@/lib/api/tools';
import { useBusinessStore } from '@/lib/stores/business';
import { STALE_TIME_5_MIN } from '@/lib/constants/cacheTTL';
import { PLATFORM_DISPLAY_ORDER } from '@/lib/platforms';
import type { Tool } from '@/lib/schemas';

// staleTime 5 minutes — the registry does not change mid-session and the
// settings/project-edit pages need not refetch on every mount.
export const TOOLS_STALE_TIME_MS = STALE_TIME_5_MIN;

export function toolsQueryKey(activeBusinessId: string | null) {
  return ['businesses', activeBusinessId, 'tools'] as const;
}

export function useTools() {
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  return useQuery<Tool[]>({
    queryKey: toolsQueryKey(activeBusinessId),
    queryFn: () => fetchTools(activeBusinessId!),
    enabled: !!activeBusinessId,
    staleTime: TOOLS_STALE_TIME_MS,
  });
}

// UI platform buckets for grouping. Single source of truth for the
// tool-bearing platform set — `PlatformKey`, `toPlatformKey`'s Set, and
// `groupByPlatform`'s seed all derive from this tuple, so adding a new
// tool-bearing platform is one line, not four.
const TOOL_BEARING_PLATFORMS = ['telegram', 'vk', 'yandex_business', 'google_business'] as const;
type ToolBearingPlatform = (typeof TOOL_BEARING_PLATFORMS)[number];
export type PlatformKey = ToolBearingPlatform | 'other';

const toolBearingKeys: ReadonlySet<string> = new Set(TOOL_BEARING_PLATFORMS);

// flatMap rather than filter so the narrowing from PlatformId → PlatformKey
// is explicit. PlatformId is the registry-wide type (includes 2gis/avito);
// PlatformKey is the tool-bearing subset. TS cannot infer the narrowing from
// a Set membership check alone.
export const TOOL_PLATFORM_ORDER: PlatformKey[] = PLATFORM_DISPLAY_ORDER.flatMap((id) =>
  toolBearingKeys.has(id) ? [id as PlatformKey] : []
);

// toPlatformKey maps raw backend platform strings (as returned by
// GET /api/v1/tools) to stable UI keys. Unknown strings bucket as 'other'.
export function toPlatformKey(platform: string): PlatformKey {
  return toolBearingKeys.has(platform) ? (platform as PlatformKey) : 'other';
}

export function groupByPlatform(tools: Tool[]): Record<PlatformKey, Tool[]> {
  const result = Object.fromEntries(
    [...TOOL_BEARING_PLATFORMS, 'other' as const].map((k) => [k, [] as Tool[]])
  ) as Record<PlatformKey, Tool[]>;
  for (const t of tools) {
    result[toPlatformKey(t.platform)].push(t);
  }
  return result;
}

// findToolsForIntegration filters the registry to tools belonging to a given
// platform (matched on the raw backend `platform` field).
export function findToolsForIntegration(tools: Tool[], platform: string): Tool[] {
  return tools.filter((t) => t.platform === platform);
}

// Convenience helper: returns the tool NAMES for a given platform.
export function toolNamesForPlatform(tools: Tool[], platform: string): string[] {
  return findToolsForIntegration(tools, platform).map((t) => t.name);
}
