'use client';

import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseMutationResult,
  type UseQueryResult,
} from '@tanstack/react-query';
import { fetchMembers, listRoles, removeMember, updateMemberRole } from '@/lib/api/members';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { BUSINESS_LIST_QUERY_KEY } from '@/lib/hooks/useBusinessList';
import type { Member, Role } from '@/lib/schemas';

// React Query hooks for the team-members surface. Mutations follow the
// mutate-then-invalidate pattern per CONTEXT D-07 — no optimistic updates,
// no setQueryData. Component layer (Plan 05) handles error→toast mapping
// via lib/resolveErrorMap.mapMemberError.
//
// All hooks accept `businessId: string | null` so callers can pass
// `useBusinessStore.activeBusinessId` verbatim; queries gate on
// `enabled: !!businessId` and mutations rely on the bizApi() helper to
// throw if a mutation is fired before activeBusinessId is set.

export function useMembers(businessId: string | null): UseQueryResult<Member[]> {
  return useQuery<Member[]>({
    queryKey: QUERY_KEYS.MEMBERS(businessId),
    queryFn: () => fetchMembers(businessId as string),
    enabled: !!businessId,
  });
}

export function useRoles(businessId: string | null): UseQueryResult<Role[]> {
  return useQuery<Role[]>({
    queryKey: QUERY_KEYS.ROLES(businessId),
    queryFn: () => listRoles(businessId as string),
    enabled: !!businessId,
  });
}

export interface UpdateMemberRoleVars {
  userId: string;
  roleId: string;
}

export function useUpdateMemberRole(
  businessId: string | null
): UseMutationResult<Member, Error, UpdateMemberRoleVars> {
  const qc = useQueryClient();
  return useMutation<Member, Error, UpdateMemberRoleVars>({
    mutationFn: ({ userId, roleId }) => updateMemberRole(businessId as string, userId, roleId),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: QUERY_KEYS.MEMBERS(businessId) });
    },
  });
}

export function useRemoveMember(businessId: string | null): UseMutationResult<void, Error, string> {
  const qc = useQueryClient();
  return useMutation<void, Error, string>({
    mutationFn: (userId) => removeMember(businessId as string, userId),
    onSuccess: () => {
      // D-07: invalidate the per-business members list AND the user's
      // overall business list. Self-removal (or removal of the last admin)
      // can change which businesses the current user has access to, so the
      // switcher data must refresh too.
      void qc.invalidateQueries({ queryKey: QUERY_KEYS.MEMBERS(businessId) });
      void qc.invalidateQueries({ queryKey: BUSINESS_LIST_QUERY_KEY });
    },
  });
}
