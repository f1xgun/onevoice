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

// Phase 19 / Plan 19-02 — pin / unpin a conversation. Both mutations
// invalidate the ['conversations'] cache on success, extending the
// established Phase 18 D-10 invalidation pattern (the sidebar list + the
// ChatHeader narrow-memo selector both refresh from a single source).
export function usePinConversation() {
  const qc = useQueryClient();
  return useMutation<Conversation, Error, string>({
    mutationFn: (conversationId) => conversationsApi.pinConversation(conversationId),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: conversationsQueryKey });
    },
  });
}

export function useUnpinConversation() {
  const qc = useQueryClient();
  return useMutation<Conversation, Error, string>({
    mutationFn: (conversationId) => conversationsApi.unpinConversation(conversationId),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: conversationsQueryKey });
    },
  });
}
