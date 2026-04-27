'use client';

import { useState } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import {
  Bookmark,
  ChevronDown,
  ChevronRight,
  FolderMinus,
  MoreHorizontal,
  Plus,
} from 'lucide-react';
import { toast } from 'sonner';
import { cn } from '@/lib/utils';
import { useCreateConversation } from '@/hooks/useConversations';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
// PinChatMenuItem renders the «Закрепить» / «Открепить» context-menu entry
// (Phase 19 / Plan 19-02 / UI-03 — locked Russian copy per 19-CONTEXT.md).
import { PinChatMenuItem } from '@/components/chat/PinChatMenuItem';
import type { Conversation } from '@/lib/conversations';

interface Props {
  conversations: Conversation[];
  activeConversationId?: string;
  onNavigate?: () => void;
}

const MAX_VISIBLE = 20;

export function UnassignedBucket({ conversations, activeConversationId, onNavigate }: Props) {
  const [collapsed, setCollapsed] = useState(false);
  const router = useRouter();
  const createConversation = useCreateConversation();

  const count = conversations.length;
  const visible = conversations.slice(0, MAX_VISIBLE);

  async function handleCreate() {
    try {
      const conv = await createConversation.mutateAsync({
        title: 'Новый диалог',
        projectId: null,
      });
      onNavigate?.();
      router.push(`/chat/${conv.id}`);
    } catch {
      toast.error('Не удалось создать чат');
    }
  }

  return (
    <div className="group/bucket">
      <div className="flex items-center gap-1 rounded-md px-2 py-1.5 text-sm text-gray-300 hover:bg-gray-800">
        <button
          type="button"
          onClick={() => setCollapsed((v) => !v)}
          className="flex flex-1 items-center gap-2 text-left"
          aria-expanded={!collapsed}
          aria-label={collapsed ? 'Развернуть «Без проекта»' : 'Свернуть «Без проекта»'}
        >
          {collapsed ? (
            <ChevronRight size={12} className="shrink-0 text-gray-500" />
          ) : (
            <ChevronDown size={12} className="shrink-0 text-gray-500" />
          )}
          <FolderMinus size={12} className="shrink-0 text-gray-500" />
          <span className="flex-1 truncate italic text-gray-400">Без проекта</span>
          <span className="text-xs text-gray-500">· {count}</span>
        </button>
        <button
          type="button"
          onClick={handleCreate}
          disabled={createConversation.isPending}
          aria-label="Новый чат без проекта"
          title="Новый чат без проекта"
          className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-gray-400 opacity-0 transition-opacity hover:bg-gray-700 hover:text-white focus-visible:opacity-100 group-hover/bucket:opacity-100 md:h-8 md:w-8"
        >
          <Plus size={14} />
        </button>
      </div>

      {!collapsed && (
        <div className="ml-5 mt-0.5 space-y-0.5">
          {visible.length === 0 ? (
            <p className="px-2 py-1 text-xs italic text-gray-500">Чаты без проекта</p>
          ) : (
            visible.map((conv) => {
              const pinned = conv.pinnedAt != null;
              return (
                <div key={conv.id} className="group/row flex items-center">
                  <Link
                    href={`/chat/${conv.id}`}
                    onClick={onNavigate}
                    className={cn(
                      'flex flex-1 items-center gap-1 truncate rounded-md px-2 py-1 text-xs transition-colors',
                      conv.id === activeConversationId
                        ? 'bg-gray-700 text-white'
                        : 'text-gray-400 hover:bg-gray-800 hover:text-white'
                    )}
                  >
                    {/* Phase 19 / Plan 19-02 / D-05 — bookmark indicator on
                        pinned rows; chats in «Без проекта» get NO ProjectChip
                        in the pinned row (the indicator alone disambiguates). */}
                    {pinned && (
                      <Bookmark size={10} className="shrink-0 text-yellow-400" aria-hidden />
                    )}
                    <span className="flex-1 truncate">{conv.title}</span>
                  </Link>
                  <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                      <button
                        type="button"
                        aria-label={`Меню чата «${conv.title || 'Новый диалог'}»`}
                        className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md text-gray-400 opacity-0 transition-opacity hover:bg-gray-700 hover:text-white focus-visible:opacity-100 group-hover/row:opacity-100"
                      >
                        <MoreHorizontal size={12} />
                      </button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end">
                      <PinChatMenuItem conversationId={conv.id} pinned={pinned} />
                    </DropdownMenuContent>
                  </DropdownMenu>
                </div>
              );
            })
          )}
          {count > MAX_VISIBLE && (
            <p className="px-2 py-1 text-xs text-gray-500">…и ещё {count - MAX_VISIBLE}</p>
          )}
        </div>
      )}
    </div>
  );
}
