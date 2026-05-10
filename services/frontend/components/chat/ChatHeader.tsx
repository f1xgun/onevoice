'use client';

import { memo, type ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Bookmark, MoreHorizontal } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { useBusinessStore } from '@/lib/stores/business';
import { cn } from '@/lib/utils';
import {
  conversationsQueryKey,
  usePinConversation,
  useUnpinConversation,
} from '@/hooks/useConversations';
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
  // useQuery subscription here — isolation tests assert exact commit
  // counts that scale with the number of useQuery hooks. Render the menu
  // only when these primitives are present.
  menuTitle?: string;
  menuTitleStatus?: TitleStatus;
  menuProjectId?: string | null;
}

/**
 * USER OVERRIDE structural mitigation.
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
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const tChat = useTranslations('chat');
  const fallback = tChat('newConversation');
  const { data } = useQuery<Conversation[], Error, string>({
    queryKey: conversationsQueryKey(activeBusinessId),
    queryFn: () =>
      bizApi(activeBusinessId!)
        .get<Conversation[]>(BIZ_API_PATHS.CONVERSATIONS.ROOT)
        .then((r) => r.data),
    select: (list) => {
      const conv = list.find((c) => c.id === conversationId);
      if (!conv) return '';
      // Fallback encapsulated here so the header and the sidebar share
      // exactly one definition of "what should the title look like right now?"
      return conv.title === '' || conv.titleStatus === 'auto_pending' ? fallback : conv.title;
    },
    enabled: !!conversationId && !!activeBusinessId,
  });
  return data ?? '';
}

/**
 * Narrow-memo selector for the pinned state. The `select` projection returns
 * a primitive `boolean`, so a cache mutation that changes any UNRELATED field
 * (title of a different chat, lastMessageAt of this chat, etc.) does not
 * re-render the bookmark button. Same isolation contract as
 * useConversationTitle above.
 */
function useConversationPinned(conversationId: string): boolean {
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const { data } = useQuery<Conversation[], Error, boolean>({
    queryKey: conversationsQueryKey(activeBusinessId),
    queryFn: () =>
      bizApi(activeBusinessId!)
        .get<Conversation[]>(BIZ_API_PATHS.CONVERSATIONS.ROOT)
        .then((r) => r.data),
    select: (list) => list.find((c) => c.id === conversationId)?.pinnedAt != null,
    enabled: !!conversationId && !!activeBusinessId,
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
  const tHeader = useTranslations('chat.header');
  const showMenu = menuTitle !== undefined && menuProjectId !== undefined;

  return (
    <div className="flex h-14 shrink-0 items-center justify-between gap-3 border-b border-line bg-paper-raised px-6">
      <span className="truncate text-[15px] font-medium text-ink">{title}</span>
      <div className="flex items-center gap-2">
        <button
          type="button"
          onClick={() => {
            if (pinned) unpinMutation.mutate(conversationId);
            else pinMutation.mutate(conversationId);
          }}
          aria-label={pinned ? tHeader('unpinAria') : tHeader('pinAria')}
          title={pinned ? tHeader('unpinAria') : tHeader('pinAria')}
          className="flex h-8 w-8 items-center justify-center rounded-md text-ink-soft transition-colors hover:bg-paper-sunken hover:text-ink disabled:opacity-50"
          disabled={pinMutation.isPending || unpinMutation.isPending}
        >
          <Bookmark size={16} className={cn(pinned ? 'fill-ochre text-ochre' : 'text-ink-soft')} />
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
                aria-label={tHeader('menuAria')}
                title={tHeader('menuTitle')}
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
