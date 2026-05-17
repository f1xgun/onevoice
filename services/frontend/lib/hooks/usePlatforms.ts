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

  // Request-scoped platform metadata (B1). Used both to enrich the wire
  // payload and to synthesize the offline fallback below.
  const platformMeta = usePlatformMeta();

  const enriched = useMemo<EnrichedPlatform[]>(() => {
    // Fallback while the request is in-flight or if it fails outright.
    // Status comes from PlatformMeta.defaultStatus, which mirrors the
    // at-rest state of the Go registry. This avoids advertising connect
    // cards for non-MVP platforms during the brief window before
    // /api/v1/platforms resolves (or forever, if the deployment is
    // misconfigured and the request 500s every time). The real
    // per-deployment status — including oauth_not_configured downgrades —
    // arrives on the next render.
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
    // Backend already returns display order; we only attach UI metadata
    // for ids we know about. Unknown ids are dropped — adding a platform
    // to the backend without updating PLATFORM_STATIC_META is a
    // programming error and the empty UI is the loud failure mode that
    // surfaces it.
    return query.data.flatMap((p) => {
      const meta = platformMeta[p.id as PlatformId];
      if (!meta) return [];
      return [{ ...p, ...meta }];
    });
  }, [query.data, platformMeta]);

  return { ...query, platforms: enriched };
}
