import { z } from 'zod';
import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { toolSchema, type Tool } from '@/lib/schemas';

// fetchTools calls GET /api/v1/businesses/{id}/tools and validates the registry response.
// /tools is now business-scoped per Plan 02-04 router migration.
export async function fetchTools(activeBusinessId: string): Promise<Tool[]> {
  const { data } = await bizApi(activeBusinessId).get<unknown>(BIZ_API_PATHS.TOOLS.ROOT);
  return z.array(toolSchema).parse(data);
}
