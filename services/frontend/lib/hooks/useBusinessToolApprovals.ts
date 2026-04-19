'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  fetchBusinessToolApprovals,
  updateBusinessToolApprovals,
} from '@/lib/api/businessApprovals';
import type { ToolApprovals } from '@/lib/schemas';

export const businessToolApprovalsQueryKey = (businessId: string) =>
  ['business', businessId, 'toolApprovals'] as const;

export function useBusinessToolApprovals(businessId: string) {
  return useQuery<ToolApprovals>({
    queryKey: businessToolApprovalsQueryKey(businessId),
    queryFn: () => fetchBusinessToolApprovals(businessId),
    enabled: !!businessId,
  });
}

export function useUpdateBusinessToolApprovals(businessId: string) {
  const qc = useQueryClient();
  return useMutation<ToolApprovals, Error, ToolApprovals>({
    mutationFn: (approvals) => updateBusinessToolApprovals(businessId, approvals),
    onSuccess: (data) => {
      qc.setQueryData(businessToolApprovalsQueryKey(businessId), data);
      void qc.invalidateQueries({ queryKey: businessToolApprovalsQueryKey(businessId) });
    },
  });
}
