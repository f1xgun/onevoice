import { z } from 'zod';
import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { toolSchema, type Tool } from '@/lib/schemas';

export async function fetchTools(activeBusinessId: string): Promise<Tool[]> {
  const { data } = await bizApi(activeBusinessId).get<unknown>(BIZ_API_PATHS.TOOLS.ROOT);
  return z.array(toolSchema).parse(data);
}
