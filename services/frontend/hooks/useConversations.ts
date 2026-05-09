'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import * as conversationsApi from '@/lib/conversations';
import type { Conversation } from '@/lib/conversations';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';

export const conversationsQueryKey = QUERY_KEYS.CONVERSATIONS;

// Poll cadence while any chat sits in `auto_pending` — see refetchInterval
// callback below for context. 2 s lines up with the auto-titler's typical
// 3–8 s server-side latency: too aggressive doubles backend load, too lax
// makes the new title feel laggy after the user sends their first message.
const TITLE_POLL_INTERVAL_MS = 2000;

export function useConversationsQuery() {
  return useQuery<Conversation[]>({
    queryKey: conversationsQueryKey,
    queryFn: conversationsApi.listConversations,
    // The auto-titler is fire-and-forget on the server:
    // POST /conversations/:id/regenerate-title returns 200 immediately and
    // a goroutine writes the title 3-8 s later. Same for the implicit
    // auto-title that fires after the first user message. Poll while ANY
    // chat sits in `auto_pending` so the new title shows up without the
    // user having to refresh. Polling auto-stops once every chat resolves
    // to `auto` or `manual`.
    refetchInterval: (query) => {
      const data = query.state.data;
      if (data && data.some((c) => c.titleStatus === 'auto_pending')) {
        return TITLE_POLL_INTERVAL_MS;
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
      // row in the sidebar (QUERY_KEYS.PROJECT_CONVERSATION_COUNT(id)).
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
      // QUERY_KEYS.PROJECT_CONVERSATION_COUNT(id) (and the project list
      // header re-renders if the project changes). Move shifts the count
      // on BOTH the source and destination — invalidate the whole
      // ['projects'] prefix so every dependent count refetches.
      void qc.invalidateQueries({ queryKey: ['projects'] });
    },
  });
}

// pin / unpin a conversation. Both mutations
// invalidate the QUERY_KEYS.CONVERSATIONS cache on success, extending the
// established invalidation pattern (the sidebar list + the
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

// POST /conversations/:id/regenerate-title.
// 409 surfaces a server-supplied locked Russian copy;
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
      // Per-project chat count lives at QUERY_KEYS.PROJECT_CONVERSATION_COUNT(id).
      // Invalidate the whole ['projects'] prefix so the deleted chat's
      // project row re-fetches its count.
      void qc.invalidateQueries({ queryKey: ['projects'] });
    },
  });
}
