import { useTranslations } from 'next-intl';

import { SYSTEM_ROLE_ORDER, type SystemRoleName } from '@/lib/constants/roles';

function isSystemRole(name: string): name is SystemRoleName {
  return (SYSTEM_ROLE_ORDER as readonly string[]).includes(name);
}

/**
 * Returns a mapper from a backend role name to its display label. The API
 * stores system role names as English literals (owner/admin/editor/viewer);
 * this localizes those four via `roles.list.systemLabels.*` so they read in
 * the active locale. Custom roles render their stored name verbatim.
 *
 * Use on every non-pill surface that shows a role name (role dropdowns,
 * system-roles list, reassign picker). RolePill keeps its own lowercase
 * mono-kicker variant via `team.roles.*`.
 */
export function useRoleLabel(): (roleName: string) => string {
  const tSystem = useTranslations('roles.list.systemLabels');
  return (roleName: string) => (isSystemRole(roleName) ? tSystem(roleName) : roleName);
}
