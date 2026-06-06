'use client';

import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseMutationResult,
  type UseQueryResult,
} from '@tanstack/react-query';
import { fetchMembers, removeMember, updateMemberRole } from '@/lib/api/members';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { BUSINESS_LIST_QUERY_KEY } from '@/lib/hooks/useBusinessList';
import type { Member } from '@/lib/schemas';

// Re-exported so existing import sites keep compiling — the implementation
// lives next to the role mutations in useRoles.ts.
export { useRoles } from '@/lib/hooks/useRoles';

// Mutations follow mutate-then-invalidate; no optimistic updates, no
// setQueryData. All hooks accept `businessId: string | null` so callers
// can pass `useBusinessStore.activeBusinessId` verbatim.

export function useMembers(businessId: string | null): UseQueryResult<Member[]> {
  return useQuery<Member[]>({
    queryKey: QUERY_KEYS.MEMBERS(businessId),
    queryFn: () => fetchMembers(businessId as string),
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
      void qc.invalidateQueries({ queryKey: QUERY_KEYS.MEMBERS(businessId) });
      void qc.invalidateQueries({ queryKey: BUSINESS_LIST_QUERY_KEY });
    },
  });
}
