'use client';

import { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { fetchPlatforms, type Platform, type PlatformStatus } from '@/lib/api/platforms';
import { STALE_TIME_5_MIN } from '@/lib/constants/cacheTTL';
import {
  PLATFORM_DISPLAY_ORDER,
  usePlatformMeta,
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
export const PLATFORMS_STALE_TIME_MS = STALE_TIME_5_MIN;

export function usePlatforms() {
  const query = useQuery<Platform[]>({
    queryKey: PLATFORMS_QUERY_KEY,
    queryFn: fetchPlatforms,
    staleTime: PLATFORMS_STALE_TIME_MS,
  });

  const platformMeta = usePlatformMeta();

  const enriched = useMemo<EnrichedPlatform[]>(() => {
    if (!query.data) {
      return PLATFORM_DISPLAY_ORDER.map((id) => {
        const meta = platformMeta[id];
        return {
          id,
          name: meta.fullLabel,
          description: '',
          status: meta.defaultStatus as PlatformStatus,
          ...meta,
        };
      });
    }
    return query.data.flatMap((p) => {
      const meta = platformMeta[p.id as PlatformId];
      if (!meta) return [];
      return [{ ...p, ...meta }];
    });
  }, [query.data, platformMeta]);

  return { ...query, platforms: enriched };
}
