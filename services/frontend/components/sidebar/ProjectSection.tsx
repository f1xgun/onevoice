'use client';

import { useState } from 'react';
import type { RefObject } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { Bookmark, ChevronDown, ChevronRight, FolderOpen, MoreHorizontal, Plus } from 'lucide-react';
import { toast } from 'sonner';
import { cn } from '@/lib/utils';
import { useCreateConversation } from '@/hooks/useConversations';
import { useRovingTabIndex } from '@/hooks/useRovingTabIndex';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
// PinChatMenuItem renders the «Закрепить» / «Открепить» context-menu entry
// (Phase 19 / Plan 19-02 / UI-03 — locked Russian copy per 19-CONTEXT.md).
import { PinChatMenuItem } from '@/components/chat/PinChatMenuItem';
import type { Conversation } from '@/lib/conversations';
import type { Project } from '@/types/project';

interface Props {
  project: Project;
  conversations: Conversation[];
  activeConversationId?: string;
  onNavigate?: () => void;
}

const MAX_VISIBLE = 20;

export function ProjectSection({
  project,
  conversations,
  activeConversationId,
  onNavigate,
}: Props) {
  const [collapsed, setCollapsed] = useState(false);
  const router = useRouter();
  const createConversation = useCreateConversation();

  const count = conversations.length;
  const visible = conversations.slice(0, MAX_VISIBLE);

  // Phase 19 / Plan 19-05 / D-17 — roving-tabindex on the chat-list portion.
  // Tab enters the list once (lands on the first row), ↑/↓/Home/End navigate.
  // The project header (chevron / link / +) sits OUTSIDE the container — it
  // remains a separate Tab stop, which D-17 explicitly requires.
  const { containerRef, onKeyDown } = useRovingTabIndex(visible.length);

  async function handleCreate() {
    try {
      const conv = await createConversation.mutateAsync({
        title: 'Новый диалог',
        projectId: project.id,
      });
      onNavigate?.();
      router.push(`/chat/${conv.id}`);
    } catch {
      toast.error('Не удалось создать чат');
    }
  }

  return (
    <div className="group/project">
      <div className="flex items-center gap-1 rounded-md px-2 py-1.5 text-sm text-gray-300 hover:bg-gray-800">
        <button
          type="button"
          onClick={() => setCollapsed((v) => !v)}
          className="shrink-0 text-gray-500 hover:text-white"
          aria-expanded={!collapsed}
          aria-label={collapsed ? `Развернуть «${project.name}»` : `Свернуть «${project.name}»`}
        >
          {collapsed ? <ChevronRight size={12} /> : <ChevronDown size={12} />}
        </button>
        <Link
          href={`/projects/${project.id}`}
          onClick={onNavigate}
          className="flex flex-1 items-center gap-2 truncate"
        >
          <FolderOpen size={12} className="shrink-0 text-gray-500" />
          <span className="flex-1 truncate">{project.name}</span>
          <span className="text-xs text-gray-500">· {count}</span>
        </Link>
        <button
          type="button"
          onClick={handleCreate}
          disabled={createConversation.isPending}
          aria-label={`Новый чат в проекте «${project.name}»`}
          title={`Новый чат в проекте «${project.name}»`}
          className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-gray-400 opacity-0 transition-opacity hover:bg-gray-700 hover:text-white focus-visible:opacity-100 group-hover/project:opacity-100 md:h-8 md:w-8"
        >
          <Plus size={14} />
        </button>
      </div>

      {!collapsed && (
        <div
          ref={containerRef as RefObject<HTMLDivElement>}
          onKeyDown={onKeyDown}
          role="listbox"
          aria-label={`Чаты проекта «${project.name}»`}
          className="ml-5 mt-0.5 space-y-0.5"
        >
          {visible.length === 0 ? (
            <p className="px-2 py-1 text-xs italic text-gray-500">В проекте пока нет чатов</p>
          ) : (
            visible.map((conv, i) => {
              const pinned = conv.pinnedAt != null;
              return (
                <div key={conv.id} className="group/row flex items-center">
                  <Link
                    href={`/chat/${conv.id}`}
                    onClick={onNavigate}
                    data-roving-item
                    tabIndex={i === 0 ? 0 : -1}
                    role="option"
                    aria-selected={conv.id === activeConversationId}
                    className={cn(
                      'flex flex-1 items-center gap-1 truncate rounded-md px-2 py-1 text-xs transition-colors',
                      conv.id === activeConversationId
                        ? 'bg-gray-700 text-white'
                        : 'text-gray-400 hover:bg-gray-800 hover:text-white'
                    )}
                  >
                    {/* Phase 19 / Plan 19-02 / D-05 — pinned chats render the
                        same bookmark indicator under their own project, so the
                        duplication of the pinned row is visually obvious. */}
                    {pinned && (
                      <Bookmark size={10} className="shrink-0 text-yellow-400" aria-hidden />
                    )}
                    <span className="flex-1 truncate">{conv.title || 'Новый диалог'}</span>
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
