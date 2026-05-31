// The DTO mirrors the wire shape produced by audit_log.go. Fields are
// snake_case to match the backend JSON envelope verbatim — the page never
// re-shapes them, so keeping the names identical reduces translation
// surface in tests and dev tools.

export type AuditCategory = 'rbac' | 'auth' | 'integration' | 'business' | 'project' | 'other';

export interface AuditLogDTO {
  id: string;
  action: string;
  action_category: AuditCategory;
  resource: string;
  business_id: string | null;
  actor_id: string | null;
  actor_email: string | null;
  actor_display_name: string | null;
  details: unknown;
  created_at: string;
}

export interface AuditLogListResponse {
  items: AuditLogDTO[];
  next_cursor: string | null;
}

export interface AuditFilters {
  category?: AuditCategory | 'all';
  action?: string;
  actorID?: string;
  from?: string; // ISO 8601
  to?: string; // ISO 8601
}

// AUDIT_ACTIONS MUST stay in sync with pkg/audit/actions.go.
// actionLabels.ts imports this constant (never re-declares it), and
// `audit.actions.<key>` in messages/ru.json is keyed off this tuple via
// actionToI18nKey(). Adding a new action means editing this list and adding
// one entry to messages/ru.json `audit.actions`.
export const AUDIT_ACTIONS = [
  'rbac.role_granted',
  'rbac.member_removed',
  'rbac.role_created',
  'rbac.role_updated',
  'rbac.role_deleted',
  'rbac.invitation_created',
  'rbac.invitation_revoked',
  'rbac.invitation_accepted',
  'auth.login_success',
  'auth.login_failed',
  'auth.logout',
  'auth.password_changed',
  'auth.user_registered',
  'integration.connected',
  'integration.disconnected',
  'integration.token_rotated',
  'business.created',
  'business.updated',
  'project.created',
  'project.updated',
  'project.deleted',
] as const;

export type AuditAction = (typeof AUDIT_ACTIONS)[number];
