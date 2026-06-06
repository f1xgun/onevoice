'use client';

import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseMutationResult,
  type UseQueryResult,
} from '@tanstack/react-query';
import {
  acceptInvitation,
  createInvitation,
  listInvitations,
  previewInvitation,
  revokeInvitation,
  type CreateInvitationInput,
} from '@/lib/api/invitations';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { BUSINESS_LIST_QUERY_KEY } from '@/lib/hooks/useBusinessList';
import { useBusinessStore } from '@/lib/stores/business';
import type {
  InvitationAcceptResponse,
  InvitationCreateResponse,
  InvitationPreview,
  PendingInvitation,
} from '@/lib/schemas';

// React Query hooks for the invitation surfaces. All mutations follow
// mutate-then-invalidate — no optimistic updates.

export function useInvitations(businessId: string | null): UseQueryResult<PendingInvitation[]> {
  return useQuery<PendingInvitation[]>({
    queryKey: QUERY_KEYS.INVITATIONS(businessId),
    queryFn: () => listInvitations(businessId as string),
    enabled: !!businessId,
  });
}

export function useInvitationPreview(
  token: string,
  enabled: boolean
): UseQueryResult<InvitationPreview> {
  return useQuery<InvitationPreview>({
    queryKey: ['invitations', token, 'preview'] as const,
    queryFn: () => previewInvitation(token),
    enabled,
    retry: false,
  });
}

export function useCreateInvitation(
  businessId: string | null
): UseMutationResult<InvitationCreateResponse, Error, CreateInvitationInput> {
  const qc = useQueryClient();
  return useMutation<InvitationCreateResponse, Error, CreateInvitationInput>({
    mutationFn: (input) => createInvitation(businessId as string, input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: QUERY_KEYS.INVITATIONS(businessId) });
    },
  });
}

export function useRevokeInvitation(
  businessId: string | null
): UseMutationResult<void, Error, string> {
  const qc = useQueryClient();
  return useMutation<void, Error, string>({
    mutationFn: (invitationId) => revokeInvitation(businessId as string, invitationId),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: QUERY_KEYS.INVITATIONS(businessId) });
    },
  });
}

// Token-scoped accept mutation. On success:
//   1. setActive(business_id) — store the new business as active BEFORE
//      invalidate so the next render of BusinessRequiredGuard sees a
//      valid id.
//   2. invalidateQueries(['businesses']) — refetch the user's membership
//      list so the switcher shows the newly-joined business.
//
// The caller (AcceptInvitePage) is responsible for router.push('/chat')
// after mutateAsync resolves. Ordering per RESEARCH Open Question #3.
export function useAcceptInvitation(): UseMutationResult<InvitationAcceptResponse, Error, string> {
  const qc = useQueryClient();
  const setActive = useBusinessStore((s) => s.setActive);
  return useMutation<InvitationAcceptResponse, Error, string>({
    mutationFn: (token) => acceptInvitation(token),
    onSuccess: (data) => {
      setActive(data.business_id);
      void qc.invalidateQueries({ queryKey: BUSINESS_LIST_QUERY_KEY });
    },
  });
}
