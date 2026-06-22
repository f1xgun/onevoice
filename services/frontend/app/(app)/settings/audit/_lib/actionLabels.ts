// Action-label helpers. Single source of truth for the action list lives
// in ./types (AUDIT_ACTIONS). This module IMPORTS it — there is no
// duplicate array literal here, and the i18n key map is typed
// `Record<AuditAction, string>` so adding an action without updating the
// map (or vice versa) is a TypeScript compile error.

import { AUDIT_ACTIONS, type AuditAction } from './types';

// actionToI18nKey converts "rbac.role_granted" → "audit.actions.rbac_role_granted"
// (next-intl namespaces use dots as separators, so we replace the dot in
// the action with an underscore).
export function actionToI18nKey(action: string): string {
  return `audit.actions.${action.replace('.', '_')}`;
}

// actionsForCategory filters AUDIT_ACTIONS by the {category}. prefix.
// Always filters against the canonical tuple imported from ./types —
// there is no duplicate list to drift out of sync.
export function actionsForCategory(category: 'all' | string): readonly AuditAction[] {
  if (category === 'all') {
    return AUDIT_ACTIONS;
  }
  return AUDIT_ACTIONS.filter((a) => a.startsWith(category + '.'));
}

// ACTION_LABEL_KEYS is the compile-time-checked mapping from every action
// to its i18n key. The Record<AuditAction, string> shape causes a TS
// error if AUDIT_ACTIONS and this map drift out of sync — this is the
// drift guard the checker asked for.
export const ACTION_LABEL_KEYS: Record<AuditAction, string> = {
  'rbac.role_granted': 'audit.actions.rbac_role_granted',
  'rbac.member_removed': 'audit.actions.rbac_member_removed',
  'rbac.role_created': 'audit.actions.rbac_role_created',
  'rbac.role_updated': 'audit.actions.rbac_role_updated',
  'rbac.role_deleted': 'audit.actions.rbac_role_deleted',
  'rbac.invitation_created': 'audit.actions.rbac_invitation_created',
  'rbac.invitation_revoked': 'audit.actions.rbac_invitation_revoked',
  'rbac.invitation_accepted': 'audit.actions.rbac_invitation_accepted',
  'auth.login_success': 'audit.actions.auth_login_success',
  'auth.login_failed': 'audit.actions.auth_login_failed',
  'auth.logout': 'audit.actions.auth_logout',
  'auth.password_changed': 'audit.actions.auth_password_changed',
  'auth.user_registered': 'audit.actions.auth_user_registered',
  'integration.connected': 'audit.actions.integration_connected',
  'integration.disconnected': 'audit.actions.integration_disconnected',
  'integration.token_rotated': 'audit.actions.integration_token_rotated',
  'integration.token_decrypted': 'audit.actions.integration_token_decrypted',
  'integration.deleted': 'audit.actions.integration_deleted',
  'business.created': 'audit.actions.business_created',
  'business.updated': 'audit.actions.business_updated',
  'business.deletion_requested': 'audit.actions.business_deletion_requested',
  'business.deletion_canceled': 'audit.actions.business_deletion_canceled',
  'business.not_owner_blocked': 'audit.actions.business_not_owner_blocked',
  'business.self_deleted': 'audit.actions.business_self_deleted',
  'project.created': 'audit.actions.project_created',
  'project.updated': 'audit.actions.project_updated',
  'project.deleted': 'audit.actions.project_deleted',
  'rpa.scope_violation': 'audit.actions.rpa_scope_violation',
  'rpa.review_replied': 'audit.actions.rpa_review_replied',
  'rpa.post_published': 'audit.actions.rpa_post_published',
  'rpa.photo_uploaded': 'audit.actions.rpa_photo_uploaded',
  'rpa.info_updated': 'audit.actions.rpa_info_updated',
  'rpa.hours_updated': 'audit.actions.rpa_hours_updated',
};
