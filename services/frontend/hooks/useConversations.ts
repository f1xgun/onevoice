'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import * as conversationsApi from '@/lib/conversations';
import type { Conversation } from '@/lib/conversations';

export const conversationsQueryKey = ['conversations'] as const;

export function useConversationsQuery() {
  return useQuery<Conversation[]>({
    queryKey: conversationsQueryKey,
    queryFn: conversationsApi.listConversations,
    // The auto-titler is fire-and-forget on the server (Phase 18 / TITLE-09):
    // POST /conversations/:id/regenerate-title returns 200 immediately and
    // a goroutine writes the title 3-8 s later. Same for the implicit
    // auto-title that fires after the first user message. Poll while ANY
    // chat sits in `auto_pending` so the new title shows up without the
    // user having to refresh. Polling auto-stops once every chat resolves
    // to `auto` or `manual`.
    refetchInterval: (query) => {
      const data = query.state.data;
      if (data && data.some((c) => c.titleStatus === 'auto_pending')) {
        return 2000;
      }
      return false;
    },
  });
}

export function useCreateConversation() {
  const qc = useQueryClient();
  return useMutation<Conversation, Error, { title: string; projectId?: string | null }>({
    mutationFn: (input) => conversationsApi.createConversation(input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: conversationsQueryKey });
      // New chat bumps the per-project count rendered next to the project
      // row in the sidebar (['projects', id, 'conversation-count']).
      void qc.invalidateQueries({ queryKey: ['projects'] });
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
      // Sidebar project rows show a per-project chat count from
      // ['projects', id, 'conversation-count'] (and the project list
      // header re-renders if the project changes). Move shifts the count
      // on BOTH the source and destination — invalidate the whole
      // ['projects'] prefix so every dependent count refetches.
      void qc.invalidateQueries({ queryKey: ['projects'] });
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

export function useRenameConversation() {
  const qc = useQueryClient();
  return useMutation<Conversation, Error, { id: string; title: string }>({
    mutationFn: ({ id, title }) => conversationsApi.renameConversation(id, title),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: conversationsQueryKey });
    },
  });
}

// Phase 18 / TITLE-09 / D-12 — POST /conversations/:id/regenerate-title.
// 409 surfaces a server-supplied locked Russian copy (D-02 / D-03);
// callers translate err.response.data.message through their own toast.
export function useRegenerateConversationTitle() {
  const qc = useQueryClient();
  return useMutation<Conversation, Error, string>({
    mutationFn: (id) => conversationsApi.regenerateConversationTitle(id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: conversationsQueryKey });
    },
  });
}

export function useDeleteConversation() {
  const qc = useQueryClient();
  return useMutation<void, Error, string>({
    mutationFn: (id) => conversationsApi.deleteConversation(id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: conversationsQueryKey });
      // Per-project chat count lives at ['projects', id, 'conversation-count'].
      // Invalidate the whole ['projects'] prefix so the deleted chat's
      // project row re-fetches its count.
      void qc.invalidateQueries({ queryKey: ['projects'] });
    },
  });
}
