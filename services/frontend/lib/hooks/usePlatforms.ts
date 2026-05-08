'use client';

import { useQuery } from '@tanstack/react-query';
import { fetchPlatforms, type Platform, type PlatformStatus } from '@/lib/api/platforms';
import {
  PLATFORM_META,
  PLATFORM_DISPLAY_ORDER,
  type PlatformId,
  type PlatformMeta,
} from '@/lib/platforms';

// EnrichedPlatform = backend authority (id, status, name, description) merged
// with frontend presentation (color, icon, labels). This is what every UI
// surface should consume — landing, /integrations, filter dropdowns, tool
// whitelist UI.
export interface EnrichedPlatform extends Platform, PlatformMeta {}

export const PLATFORMS_QUERY_KEY = ['platforms'] as const;

// 5 minutes — the registry only changes when the deployment is reconfigured
// or a new platform is added in code. There is no per-tenant variation.
export const PLATFORMS_STALE_TIME_MS = 5 * 60 * 1000;

// Fallback used while the request is in-flight or if it fails outright. We
// surface every known platform optimistically as "active" (or "coming_soon"
// for 2gis/avito) so the UI never flashes empty. The real status arrives on
// next render once /api/v1/platforms resolves.
function buildFallback(): EnrichedPlatform[] {
  const comingSoon = new Set<PlatformId>(['2gis', 'avito']);
  return PLATFORM_DISPLAY_ORDER.map((id) => {
    const meta = PLATFORM_META[id];
    const status: PlatformStatus = comingSoon.has(id) ? 'coming_soon' : 'active';
    return {
      id,
      name: meta.fullLabel,
      description: '',
      status,
      ...meta,
    };
  });
}

function enrich(platforms: Platform[]): EnrichedPlatform[] {
  // Backend already returns display order; we only attach UI metadata for
  // ids we know about. Unknown ids are dropped — adding a platform to the
  // backend without updating PLATFORM_META is a programming error and the
  // empty UI is the loud failure mode that surfaces it.
  return platforms.flatMap((p) => {
    const meta = PLATFORM_META[p.id as PlatformId];
    if (!meta) return [];
    return [{ ...p, ...meta }];
  });
}

export function usePlatforms() {
  const query = useQuery<Platform[]>({
    queryKey: PLATFORMS_QUERY_KEY,
    queryFn: fetchPlatforms,
    staleTime: PLATFORMS_STALE_TIME_MS,
  });

  const enriched: EnrichedPlatform[] = query.data ? enrich(query.data) : buildFallback();
  return { ...query, platforms: enriched };
}
