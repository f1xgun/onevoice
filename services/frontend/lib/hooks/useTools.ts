'use client';

import { useQuery } from '@tanstack/react-query';
import { fetchTools } from '@/lib/api/tools';
import type { Tool } from '@/lib/schemas';

// Phase 16 — live feed of the orchestrator registry (GET /api/v1/tools).
// Replaces the Phase 15 hardcoded tool list. Unscoped: the registry is
// global (not per-business), so the React Query key is plain ['tools'].
//
// staleTime 5 minutes — the registry does not change mid-session and the
// settings/project-edit pages need not refetch on every mount.
export const TOOLS_QUERY_KEY = ['tools'] as const;
export const TOOLS_STALE_TIME_MS = 5 * 60 * 1000;

export function useTools() {
  return useQuery<Tool[]>({
    queryKey: TOOLS_QUERY_KEY,
    queryFn: fetchTools,
    staleTime: TOOLS_STALE_TIME_MS,
  });
}

// UI platform buckets for grouping. Keep these stable — the consumer UIs
// (settings/tools, ProjectApprovalOverrides, ToolCheckboxGrid,
// WhitelistWarningBanner) all depend on these exact keys.
export type PlatformKey = 'telegram' | 'vk' | 'yandex_business' | 'google_business' | 'other';

// toPlatformKey maps raw backend platform strings (as returned by
// GET /api/v1/tools) to stable UI keys. Legacy `yandex_business` /
// `google_business` strings (with underscores) pass through unchanged so
// the buckets match the previous Phase 15 bucket layout.
export function toPlatformKey(platform: string): PlatformKey {
  switch (platform) {
    case 'telegram':
      return 'telegram';
    case 'vk':
      return 'vk';
    case 'yandex_business':
      return 'yandex_business';
    case 'google_business':
      return 'google_business';
    default:
      return 'other';
  }
}

export function groupByPlatform(tools: Tool[]): Record<PlatformKey, Tool[]> {
  const result: Record<PlatformKey, Tool[]> = {
    telegram: [],
    vk: [],
    yandex_business: [],
    google_business: [],
    other: [],
  };
  for (const t of tools) {
    result[toPlatformKey(t.platform)].push(t);
  }
  return result;
}

// findToolsForIntegration filters the registry to tools belonging to a given
// platform (matched on the raw backend `platform` field). Used by the
// WhitelistWarningBanner to project which tools a new integration brings.
export function findToolsForIntegration(tools: Tool[], platform: string): Tool[] {
  return tools.filter((t) => t.platform === platform);
}

// Convenience helper: returns the tool NAMES for a given platform. Used by
// consumers that previously consumed a plain string[] bucket map
// (e.g. WhitelistWarningBanner).
export function toolNamesForPlatform(tools: Tool[], platform: string): string[] {
  return findToolsForIntegration(tools, platform).map((t) => t.name);
}
