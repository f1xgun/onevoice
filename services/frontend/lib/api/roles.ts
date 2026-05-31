import { z } from 'zod';

import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { roleSchema, type Role } from '@/lib/schemas';

// Backend contracts:
//   GET    /api/v1/businesses/{id}/roles               → 200 Role[] (with member_count)
//   POST   /api/v1/businesses/{id}/roles               → 201 Role (no member_count)
//          body: { name, description, permissions }
//          409 { error: "role_name_taken" }
//          403 { error: "cannot_grant_unowned_permissions" }
//   PATCH  /api/v1/businesses/{id}/roles/{roleId}      → 200 Role
//          body: { name, description, permissions }
//          404 { error: "role_not_found" }
//          409 { error: "role_name_taken" }
//          422 { error: "system_role_immutable" | "self_lockout" }
//          403 { error: "cannot_grant_unowned_permissions" }
//   DELETE /api/v1/businesses/{id}/roles/{roleId}?reassign_to=<uuid>  → 204
//          404 { error: "role_not_found" }
//          422 { error: "role_in_use" | "last_owner" | "system_role_immutable" }
//
// Every response is parsed through the canonical zod schema so malformed
// payloads fail loudly at the API seam rather than surfacing as undefined
// deep in the UI tree.

const rolesArraySchema = z.array(roleSchema);

export async function listRoles(businessId: string): Promise<Role[]> {
  const { data } = await bizApi(businessId).get<unknown>(BIZ_API_PATHS.ROLES.ROOT);
  return rolesArraySchema.parse(data);
}

export interface CreateRoleInput {
  name: string;
  description: string;
  permissions: string[];
}

export async function createRole(businessId: string, input: CreateRoleInput): Promise<Role> {
  const { data } = await bizApi(businessId).post<unknown>(BIZ_API_PATHS.ROLES.ROOT, input);
  return roleSchema.parse(data);
}

export type UpdateRoleInput = CreateRoleInput;

export async function updateRole(
  businessId: string,
  roleId: string,
  input: UpdateRoleInput
): Promise<Role> {
  const { data } = await bizApi(businessId).patch<unknown>(
    BIZ_API_PATHS.ROLES.BY_ID(roleId),
    input
  );
  return roleSchema.parse(data);
}

// `reassignTo` is the UUID of the role to move displaced members to when the
// deleted role has member_count > 0. Pass null when the role has no members
// (the backend rejects empty reassign with role_in_use if any rows remain).
export async function deleteRole(
  businessId: string,
  roleId: string,
  reassignTo: string | null
): Promise<void> {
  const path = reassignTo
    ? `${BIZ_API_PATHS.ROLES.BY_ID(roleId)}?reassign_to=${encodeURIComponent(reassignTo)}`
    : BIZ_API_PATHS.ROLES.BY_ID(roleId);
  await bizApi(businessId).delete(path);
}
