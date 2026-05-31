import type { ErrorCode } from './chat';

export interface AgentTask {
  id: string;
  businessId: string;
  type: string;
  /**
   * Legacy Russian-only label written before i18n landed. Still the
   * fallback when displayNameKey is missing or the key is absent from
   * the active locale catalog.
   */
  displayName?: string;
  /**
   * i18n catalog key segment under `agentTasks.displayName.*` used by
   * the Tasks page to render localized titles. Present on new rows
   * (and on legacy rows after BackfillAgentTaskDisplayNameKey runs);
   * older rows omit it and surface as `displayName` instead.
   */
  displayNameKey?: string;
  status: string;
  platform: string;
  input?: unknown;
  output?: unknown;
  error?: string;
  /**
   * Typed classifier from the platform agent (locked enum). Present on rows
   * persisted on or after Phase 26-04; historical rows omit it and fall
   * through to the calm summary on the Tasks page.
   */
  errorCode?: ErrorCode;
  startedAt?: string;
  completedAt?: string;
  createdAt: string;
}

export type TaskStreamEventKind = 'task.created' | 'task.updated';

export interface TaskStreamEvent {
  kind: TaskStreamEventKind;
  task: AgentTask;
}
