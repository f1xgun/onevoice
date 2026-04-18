export type WhitelistMode = 'inherit' | 'all' | 'explicit' | 'none';

export interface Project {
  id: string;
  businessId: string;
  name: string;
  description: string;
  systemPrompt: string;
  whitelistMode: WhitelistMode;
  allowedTools: string[];
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
  quickActions: string[];
}

export type UpdateProjectInput = CreateProjectInput;
