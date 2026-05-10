import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { businessToolApprovalsResponseSchema, type ToolApprovals } from '@/lib/schemas';

// Backend contracts (plural URL via bizApi):
//   GET  /api/v1/businesses/{id}/tool-approvals → 200 { toolApprovals: {[name]: "auto"|"manual"} }
//   PUT  /api/v1/businesses/{id}/tool-approvals
//        body: { toolApprovals: { [name]: "auto" | "manual" } }
//        → 200 { toolApprovals: ... } or 400 { error: "unknown tool: X" }

export async function fetchBusinessToolApprovals(businessId: string): Promise<ToolApprovals> {
  const { data } = await bizApi(businessId).get<unknown>(BIZ_API_PATHS.TOOL_APPROVALS.ROOT);
  const parsed = businessToolApprovalsResponseSchema.parse(data);
  return parsed.toolApprovals;
}

export async function updateBusinessToolApprovals(
  businessId: string,
  toolApprovals: ToolApprovals
): Promise<ToolApprovals> {
  const { data } = await bizApi(businessId).put<unknown>(BIZ_API_PATHS.TOOL_APPROVALS.ROOT, {
    toolApprovals,
  });
  const parsed = businessToolApprovalsResponseSchema.parse(data);
  return parsed.toolApprovals;
}
