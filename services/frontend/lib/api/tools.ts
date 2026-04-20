import { z } from 'zod';
import { api } from '@/lib/api';
import { toolSchema, type Tool } from '@/lib/schemas';

// fetchTools calls GET /api/v1/tools and validates the registry response.
// Backend: services/api/internal/handler/hitl.go GetTools (see 16-07 SUMMARY).
export async function fetchTools(): Promise<Tool[]> {
  const { data } = await api.get<unknown>('/tools');
  return z.array(toolSchema).parse(data);
}
