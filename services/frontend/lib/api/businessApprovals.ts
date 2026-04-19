import { api } from '@/lib/api';
import {
  businessToolApprovalsResponseSchema,
  type ToolApprovals,
} from '@/lib/schemas';

// Backend contracts (Phase 16-07):
//   GET  /api/v1/business/{id}/tool-approvals → 200 { toolApprovals: {[name]: "auto"|"manual"} }
//   PUT  /api/v1/business/{id}/tool-approvals
//        body: { toolApprovals: { [name]: "auto" | "manual" } }
//        → 200 { toolApprovals: ... } or 400 { error: "unknown tool: X" }

export async function fetchBusinessToolApprovals(businessId: string): Promise<ToolApprovals> {
  const { data } = await api.get<unknown>(`/business/${businessId}/tool-approvals`);
  const parsed = businessToolApprovalsResponseSchema.parse(data);
  return parsed.toolApprovals;
}

export async function updateBusinessToolApprovals(
  businessId: string,
  toolApprovals: ToolApprovals
): Promise<ToolApprovals> {
  const { data } = await api.put<unknown>(`/business/${businessId}/tool-approvals`, {
    toolApprovals,
  });
  const parsed = businessToolApprovalsResponseSchema.parse(data);
  return parsed.toolApprovals;
}
