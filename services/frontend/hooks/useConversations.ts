'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import * as conversationsApi from '@/lib/conversations';
import type { Conversation } from '@/lib/conversations';

export const conversationsQueryKey = ['conversations'] as const;

export function useConversationsQuery() {
  return useQuery<Conversation[]>({
    queryKey: conversationsQueryKey,
    queryFn: conversationsApi.listConversations,
  });
}

export function useCreateConversation() {
  const qc = useQueryClient();
  return useMutation<Conversation, Error, { title: string; projectId?: string | null }>({
    mutationFn: (input) => conversationsApi.createConversation(input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: conversationsQueryKey });
    },
  });
}

export function useMoveConversation() {
  const qc = useQueryClient();
  return useMutation<
    Conversation,
    Error,
    { id: string; projectId: string | null; previousProjectId: string | null }
  >({
    mutationFn: ({ id, projectId }) => conversationsApi.moveConversation(id, projectId),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: conversationsQueryKey });
    },
  });
}
