'use client';

import { memo, type ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { api } from '@/lib/api';
import type { Conversation } from '@/lib/conversations';

interface ChatHeaderProps {
  conversationId: string;
  rightSlot?: ReactNode;
}

/**
 * D-11 USER OVERRIDE structural mitigation (Landmine 1).
 *
 *   1. useQuery `select` projection returns a primitive `string`. React Query
 *      runs `select` on every cache change, but consumers (this hook) receive
 *      a stable string reference unless the title actually changes — so an
 *      unrelated field mutation (e.g., `lastMessageAt`) does NOT trigger a
 *      re-render of this component.
 *   2. The component is wrapped in `memo`, so prop-change re-renders from the
 *      parent (`ChatWindow`) are skipped when nothing changed.
 *   3. ChatHeader is rendered as a SIBLING of MessageList and Composer in
 *      ChatWindow (not an ancestor), so a title-change re-render here cannot
 *      destroy composer focus or scroll position in the message list.
 *
 *   Together these three defences mean a title arrival via React Query
 *   invalidation flicks ONLY the header DOM. Verified in
 *   ChatHeader.isolation.test.tsx via vi.fn() + React.Profiler.onRender +
 *   toHaveBeenCalledTimes(1) after mutating an unrelated field.
 */
function useConversationTitle(conversationId: string): string {
  const { data } = useQuery<Conversation[], Error, string>({
    queryKey: ['conversations'],
    queryFn: () => api.get('/conversations').then((r) => r.data),
    select: (list) => {
      const conv = list.find((c) => c.id === conversationId);
      if (!conv) return '';
      // D-09 fallback encapsulated here so the header and the sidebar share
      // exactly one definition of "what should the title look like right now?"
      return conv.title === '' || conv.titleStatus === 'auto_pending' ? 'Новый диалог' : conv.title;
    },
    enabled: !!conversationId,
  });
  return data ?? '';
}

function ChatHeaderImpl({ conversationId, rightSlot }: ChatHeaderProps) {
  const title = useConversationTitle(conversationId);
  return (
    <div className="flex h-14 shrink-0 items-center justify-between gap-3 border-b bg-background px-4">
      <span className="truncate text-sm font-medium">{title}</span>
      {rightSlot}
    </div>
  );
}

export const ChatHeader = memo(ChatHeaderImpl);
