'use client';

import { toast } from 'sonner';
import { DropdownMenuItem } from '@/components/ui/dropdown-menu';
import { usePinConversation, useUnpinConversation } from '@/hooks/useConversations';

// PinChatMenuItem — Phase 19 / Plan 19-02 / UI-03.
//
// Single context-menu entry that flips between «Закрепить» (when the chat
// is currently unpinned) and «Открепить» (when it's pinned). Mutations
// invalidate the ['conversations'] cache (Phase 18 D-10 invalidation
// pattern extended for pin) so the sidebar PinnedSection + ChatHeader
// bookmark icon refresh from a single source on success.
//
// Russian copy is locked verbatim per 19-CONTEXT.md (Закрепить / Открепить).
interface Props {
  conversationId: string;
  pinned: boolean;
}

export function PinChatMenuItem({ conversationId, pinned }: Props) {
  const pinMutation = usePinConversation();
  const unpinMutation = useUnpinConversation();

  return (
    <DropdownMenuItem
      onSelect={(e) => {
        e.preventDefault();
        if (pinned) {
          unpinMutation.mutate(conversationId, {
            onError: () => toast.error('Не удалось открепить чат'),
          });
        } else {
          pinMutation.mutate(conversationId, {
            onError: () => toast.error('Не удалось закрепить чат'),
          });
        }
      }}
    >
      {pinned ? 'Открепить' : 'Закрепить'}
    </DropdownMenuItem>
  );
}
