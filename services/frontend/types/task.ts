export interface AgentTask {
  id: string;
  businessId: string;
  type: string;
  displayName?: string;
  status: string;
  platform: string;
  input?: unknown;
  output?: unknown;
  error?: string;
  startedAt?: string;
  completedAt?: string;
  createdAt: string;
}

export type TaskStreamEventKind = 'task.created' | 'task.updated';

export interface TaskStreamEvent {
  kind: TaskStreamEventKind;
  task: AgentTask;
}
