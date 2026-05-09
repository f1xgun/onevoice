'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import * as conversationsApi from '@/lib/conversations';
import type { Conversation } from '@/lib/conversations';
import { projectsQueryKey } from '@/hooks/useProjects';
import { useBusinessStore } from '@/lib/stores/business';

export const conversationsQueryKey = (activeBusinessId: string | null) =>
  ['businesses', activeBusinessId, 'conversations'] as const;

// Poll cadence while any chat sits in `auto_pending` — see refetchInterval
// callback below for context. 2 s lines up with the auto-titler's typical
// 3–8 s server-side latency: too aggressive doubles backend load, too lax
// makes the new title feel laggy after the user sends their first message.
const TITLE_POLL_INTERVAL_MS = 2000;

export function useConversationsQuery() {
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  return useQuery<Conversation[]>({
    queryKey: conversationsQueryKey(activeBusinessId),
    queryFn: () => conversationsApi.listConversations(activeBusinessId!),
    enabled: !!activeBusinessId,
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
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  return useMutation<Conversation, Error, { title: string; projectId?: string | null }>({
    mutationFn: (input) => conversationsApi.createConversation(activeBusinessId!, input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: conversationsQueryKey(activeBusinessId) });
      // New chat bumps the per-project count rendered next to the project
      // row in the sidebar (projectsQueryKey).
      void qc.invalidateQueries({ queryKey: projectsQueryKey(activeBusinessId) });
    },
  });
}

export function useMoveConversation() {
  const qc = useQueryClient();
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  return useMutation<
    Conversation,
    Error,
    { id: string; projectId: string | null; previousProjectId: string | null }
  >({
    mutationFn: ({ id, projectId }) =>
      conversationsApi.moveConversation(activeBusinessId!, id, projectId),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: conversationsQueryKey(activeBusinessId) });
      // Sidebar project rows show a per-project chat count. Move shifts the
      // count on BOTH source and destination — invalidate the whole prefix.
      void qc.invalidateQueries({ queryKey: projectsQueryKey(activeBusinessId) });
    },
  });
}

// pin / unpin a conversation. Both mutations
// invalidate the conversations cache on success, extending the
// established invalidation pattern (the sidebar list + the
// ChatHeader narrow-memo selector both refresh from a single source).
export function usePinConversation() {
  const qc = useQueryClient();
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  return useMutation<Conversation, Error, string>({
    mutationFn: (conversationId) =>
      conversationsApi.pinConversation(activeBusinessId!, conversationId),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: conversationsQueryKey(activeBusinessId) });
    },
  });
}

export function useUnpinConversation() {
  const qc = useQueryClient();
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  return useMutation<Conversation, Error, string>({
    mutationFn: (conversationId) =>
      conversationsApi.unpinConversation(activeBusinessId!, conversationId),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: conversationsQueryKey(activeBusinessId) });
    },
  });
}

export function useRenameConversation() {
  const qc = useQueryClient();
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  return useMutation<Conversation, Error, { id: string; title: string }>({
    mutationFn: ({ id, title }) =>
      conversationsApi.renameConversation(activeBusinessId!, id, title),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: conversationsQueryKey(activeBusinessId) });
    },
  });
}

// POST /conversations/:id/regenerate-title.
// 409 surfaces a server-supplied locked Russian copy;
// callers translate err.response.data.message through their own toast.
export function useRegenerateConversationTitle() {
  const qc = useQueryClient();
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  return useMutation<Conversation, Error, string>({
    mutationFn: (id) => conversationsApi.regenerateConversationTitle(activeBusinessId!, id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: conversationsQueryKey(activeBusinessId) });
    },
  });
}

export function useDeleteConversation() {
  const qc = useQueryClient();
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  return useMutation<void, Error, string>({
    mutationFn: (id) => conversationsApi.deleteConversation(activeBusinessId!, id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: conversationsQueryKey(activeBusinessId) });
      // Per-project chat count. Invalidate the whole businesses/id/projects prefix.
      void qc.invalidateQueries({ queryKey: projectsQueryKey(activeBusinessId) });
    },
  });
}
