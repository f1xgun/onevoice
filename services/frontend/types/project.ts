export type WhitelistMode = 'inherit' | 'all' | 'explicit' | 'none';

// Phase 16 — POLICY-06: project-level overrides of the business tool-approval
// map. Only "auto" and "manual" are valid values; inherit is encoded as KEY
// ABSENCE (Overview invariant #8). Sent as a map on PUT /projects/{id}.
export type ProjectApprovalOverrides = Record<string, 'auto' | 'manual'>;

export interface Project {
  id: string;
  businessId: string;
  name: string;
  description: string;
  systemPrompt: string;
  whitelistMode: WhitelistMode;
  allowedTools: string[];
  approvalOverrides?: ProjectApprovalOverrides;
  quickActions: string[];
  createdAt: string;
  updatedAt: string;
}

export interface CreateProjectInput {
  name: string;
  description: string;
  systemPrompt: string;
  whitelistMode: WhitelistMode;
  allowedTools: string[];
  approvalOverrides?: ProjectApprovalOverrides;
  quickActions: string[];
}

export type UpdateProjectInput = CreateProjectInput;
