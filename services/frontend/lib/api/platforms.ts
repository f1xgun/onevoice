import { z } from 'zod';
import { api } from '@/lib/api';

// Mirrors pkg/domain/platform.go — keep enum values in sync with the Go
// PlatformStatus constants.
export const platformStatusSchema = z.enum(['active', 'coming_soon', 'oauth_not_configured']);
export type PlatformStatus = z.infer<typeof platformStatusSchema>;

// i18n Phase C2: backend no longer serializes `name` / `description`.
// Both are resolved on the frontend via messages/*.json
// (platforms.fullLabel.<id>, platforms.description.<id>). Kept optional
// here for legacy clients that still consume old API builds; new wire
// responses omit the keys entirely.
export const platformSchema = z.object({
  id: z.string(),
  name: z.string().optional().default(''),
  description: z.string().optional().default(''),
  status: platformStatusSchema,
});
export type Platform = z.infer<typeof platformSchema>;

// fetchPlatforms calls the public GET /api/v1/platforms endpoint
// (services/api/internal/handler/platforms.go). Returns the canonical list
// in display order — callers should not re-sort.
export async function fetchPlatforms(): Promise<Platform[]> {
  const { data } = await api.get<unknown>('/platforms');
  return z.array(platformSchema).parse(data);
}
