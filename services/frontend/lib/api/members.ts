import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import {
  memberSchema,
  membersListSchema,
  rolesListSchema,
  type Member,
  type Role,
} from '@/lib/schemas';

// Backend contracts (Phase 2/3, plural URL via bizApi):
//   GET    /api/v1/businesses/{id}/members           → 200 Member[]
//   PATCH  /api/v1/businesses/{id}/members/{userId}  → 200 Member
//          body: { role_id: string }
//          422 { error: "last_owner" | "self_lockout" }
//          400 { error: "invalid_role_id" }
//   DELETE /api/v1/businesses/{id}/members/{userId}  → 204 (no body)
//          422 { error: "last_owner" }
//   GET    /api/v1/businesses/{id}/roles             → 200 Role[]
//
// Every response is parsed through the canonical zod schema before the
// caller sees it. Malformed payloads fail loudly at the seam rather than
// surfacing as `undefined` deep in the UI tree (threat T-04-02-01).

export async function fetchMembers(businessId: string): Promise<Member[]> {
  const { data } = await bizApi(businessId).get<unknown>(BIZ_API_PATHS.MEMBERS.ROOT);
  return membersListSchema.parse(data);
}

export async function updateMemberRole(
  businessId: string,
  userId: string,
  roleId: string
): Promise<Member> {
  const { data } = await bizApi(businessId).patch<unknown>(BIZ_API_PATHS.MEMBERS.BY_ID(userId), {
    role_id: roleId,
  });
  return memberSchema.parse(data);
}

export async function removeMember(businessId: string, userId: string): Promise<void> {
  await bizApi(businessId).delete<unknown>(BIZ_API_PATHS.MEMBERS.BY_ID(userId));
}

export async function listRoles(businessId: string): Promise<Role[]> {
  const { data } = await bizApi(businessId).get<unknown>(BIZ_API_PATHS.ROLES.ROOT);
  return rolesListSchema.parse(data);
}
