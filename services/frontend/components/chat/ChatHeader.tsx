'use client';

import { memo, type ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Bookmark, MoreHorizontal } from 'lucide-react';
import { api } from '@/lib/api';
import { cn } from '@/lib/utils';
import { usePinConversation, useUnpinConversation } from '@/hooks/useConversations';
import { ChatRowMenu } from '@/components/chat/ChatRowMenu';
import type { Conversation, TitleStatus } from '@/lib/conversations';

interface ChatHeaderProps {
  conversationId: string;
  rightSlot?: ReactNode;
  // Fired after the chat has been deleted via the actions menu. The chat
  // owner (chat/[id]/page.tsx) wires this to router.push('/chat'). Kept
  // optional so existing isolation tests can mount ChatHeader without a
  // Next.js router context.
  onConversationDeleted?: () => void;
  // Menu data passed in as primitive props from ChatWindow (which already
  // owns the per-conversation query). ChatHeader does NOT add a third
  // useQuery subscription here — D-11 isolation tests assert exact commit
  // counts that scale with the number of useQuery hooks. Render the menu
  // only when these primitives are present.
  menuTitle?: string;
  menuTitleStatus?: TitleStatus;
  menuProjectId?: string | null;
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

/**
 * Phase 19 / Plan 19-02 / D-11 mitigation — narrow-memo selector for the
 * pinned state. The `select` projection returns a primitive `boolean`, so a
 * cache mutation that changes any UNRELATED field (title of a different
 * chat, lastMessageAt of this chat, etc.) does not re-render the bookmark
 * button. Same isolation contract as useConversationTitle above.
 */
function useConversationPinned(conversationId: string): boolean {
  const { data } = useQuery<Conversation[], Error, boolean>({
    queryKey: ['conversations'],
    queryFn: () => api.get('/conversations').then((r) => r.data),
    select: (list) => list.find((c) => c.id === conversationId)?.pinnedAt != null,
    enabled: !!conversationId,
  });
  return data ?? false;
}

function ChatHeaderImpl({
  conversationId,
  rightSlot,
  onConversationDeleted,
  menuTitle,
  menuTitleStatus,
  menuProjectId,
}: ChatHeaderProps) {
  const title = useConversationTitle(conversationId);
  const pinned = useConversationPinned(conversationId);
  const pinMutation = usePinConversation();
  const unpinMutation = useUnpinConversation();
  const showMenu = menuTitle !== undefined && menuProjectId !== undefined;

  return (
    <div className="flex h-14 shrink-0 items-center justify-between gap-3 border-b bg-background px-4">
      <span className="truncate text-sm font-medium">{title}</span>
      <div className="flex items-center gap-2">
        <button
          type="button"
          onClick={() => {
            if (pinned) unpinMutation.mutate(conversationId);
            else pinMutation.mutate(conversationId);
          }}
          aria-label={pinned ? 'Открепить чат' : 'Закрепить чат'}
          title={pinned ? 'Открепить чат' : 'Закрепить чат'}
          className="flex h-8 w-8 items-center justify-center rounded-md hover:bg-paper-sunken disabled:opacity-50"
          disabled={pinMutation.isPending || unpinMutation.isPending}
        >
          <Bookmark
            size={16}
            className={cn(pinned ? 'fill-yellow-400 text-yellow-400' : 'text-ink-soft')}
          />
        </button>
        {showMenu && (
          <ChatRowMenu
            conversation={{
              id: conversationId,
              title: menuTitle ?? '',
              titleStatus: menuTitleStatus,
              projectId: menuProjectId ?? null,
            }}
            pinned={pinned}
            onDeleted={onConversationDeleted}
            trigger={
              <button
                type="button"
                aria-label="Меню чата"
                title="Действия"
                className="flex h-8 w-8 items-center justify-center rounded-md text-gray-500 hover:bg-gray-100 hover:text-gray-900"
              >
                <MoreHorizontal size={16} />
              </button>
            }
          />
        )}
        {rightSlot}
      </div>
    </div>
  );
}

export const ChatHeader = memo(ChatHeaderImpl);
