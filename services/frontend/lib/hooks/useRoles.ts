'use client';

import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseMutationResult,
  type UseQueryResult,
} from '@tanstack/react-query';

import {
  createRole,
  deleteRole,
  listRoles,
  updateRole,
  type CreateRoleInput,
  type UpdateRoleInput,
} from '@/lib/api/roles';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import type { Role } from '@/lib/schemas';

// React Query hooks for the roles surface. Mutations use
// mutate-then-invalidate — no optimistic updates, no setQueryData. The
// component layer maps errors to toasts via lib/resolveErrorMap.mapRoleError.
//
// All hooks accept `businessId: string | null` so callers can pass
// `useBusinessStore.activeBusinessId` verbatim; queries gate on
// `enabled: !!businessId` and mutations rely on the bizApi() helper to
// throw if a mutation is fired before activeBusinessId is set.

/**
 * Fetches roles (system + custom) for the active business with member_count
 * populated. Cache scope: ['businesses', bizId, 'roles'].
 */
export function useRoles(businessId: string | null): UseQueryResult<Role[]> {
  return useQuery<Role[]>({
    queryKey: QUERY_KEYS.ROLES(businessId),
    queryFn: () => listRoles(businessId as string),
    enabled: !!businessId,
  });
}

export function useCreateRole(
  businessId: string | null
): UseMutationResult<Role, Error, CreateRoleInput> {
  const qc = useQueryClient();
  return useMutation<Role, Error, CreateRoleInput>({
    mutationFn: (input) => createRole(businessId as string, input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: QUERY_KEYS.ROLES(businessId) });
      // Creating a role does NOT directly mutate the actor's permissions,
      // but we conservatively invalidate the per-business permissions cache
      // so any UI reading from /me/permissions stays in lockstep —
      // invalidation is cheap; staleness is not.
      void qc.invalidateQueries({ queryKey: QUERY_KEYS.PERMISSIONS(businessId) });
    },
  });
}

export interface UpdateRoleVars extends UpdateRoleInput {
  roleId: string;
}

export function useUpdateRole(
  businessId: string | null
): UseMutationResult<Role, Error, UpdateRoleVars> {
  const qc = useQueryClient();
  return useMutation<Role, Error, UpdateRoleVars>({
    mutationFn: ({ roleId, ...input }) => updateRole(businessId as string, roleId, input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: QUERY_KEYS.ROLES(businessId) });
      // Editing a role MAY change the actor's effective perms if the actor
      // happens to hold the edited role — always invalidate.
      void qc.invalidateQueries({ queryKey: QUERY_KEYS.PERMISSIONS(businessId) });
    },
  });
}

export interface DeleteRoleVars {
  roleId: string;
  reassignTo: string | null;
}

export function useDeleteRole(
  businessId: string | null
): UseMutationResult<void, Error, DeleteRoleVars> {
  const qc = useQueryClient();
  return useMutation<void, Error, DeleteRoleVars>({
    mutationFn: ({ roleId, reassignTo }) => deleteRole(businessId as string, roleId, reassignTo),
    onSuccess: () => {
      // Multi-key invalidate:
      //   - ROLES: the deleted row vanishes from the list.
      //   - PERMISSIONS: any member reassigned to a different role may now
      //     have different effective perms (including the current actor).
      //   - MEMBERS: reassignment changes the role_id column on member rows.
      void qc.invalidateQueries({ queryKey: QUERY_KEYS.ROLES(businessId) });
      void qc.invalidateQueries({ queryKey: QUERY_KEYS.PERMISSIONS(businessId) });
      void qc.invalidateQueries({ queryKey: QUERY_KEYS.MEMBERS(businessId) });
    },
  });
}
