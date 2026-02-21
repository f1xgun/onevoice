export interface AgentTask {
  id: string;
  businessId: string;
  type: string;
  status: string;
  platform: string;
  input?: unknown;
  output?: unknown;
  error?: string;
  startedAt?: string;
  completedAt?: string;
  createdAt: string;
}
