import { z } from 'zod';
import { api } from '@/lib/api';

// Mirrors pkg/domain/platform.go — keep enum values in sync with the Go
// PlatformStatus constants.
export const platformStatusSchema = z.enum(['active', 'coming_soon', 'oauth_not_configured']);
export type PlatformStatus = z.infer<typeof platformStatusSchema>;

export const platformSchema = z.object({
  id: z.string(),
  name: z.string(),
  description: z.string(),
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
