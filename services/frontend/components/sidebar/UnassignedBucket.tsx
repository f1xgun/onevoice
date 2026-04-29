'use client';

import { useState } from 'react';
import type { RefObject } from 'react';
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
import { useRovingTabIndex } from '@/hooks/useRovingTabIndex';
// ChatRowMenu hosts the per-row actions (rename, regenerate title, move,
// pin/unpin, delete). Shared with ProjectSection / PinnedSection / ChatHeader.
import { ChatRowMenu } from '@/components/chat/ChatRowMenu';
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

  // Phase 19 / Plan 19-05 / D-17 — roving-tabindex on the chat-list portion.
  // Tab enters the list once, ↑/↓/Home/End navigate. The «Без проекта»
  // header (chevron / title / +) sits OUTSIDE the container — it remains a
  // separate Tab stop, which D-17 explicitly requires.
  const { containerRef, onKeyDown } = useRovingTabIndex(visible.length);

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
      <div className="flex items-center gap-1 rounded-md px-2 py-1.5 text-sm text-ink-mid hover:bg-paper-sunken">
        <button
          type="button"
          onClick={() => setCollapsed((v) => !v)}
          className="flex flex-1 items-center gap-2 text-left"
          aria-expanded={!collapsed}
          aria-label={collapsed ? 'Развернуть «Без проекта»' : 'Свернуть «Без проекта»'}
        >
          {collapsed ? (
            <ChevronRight size={12} className="shrink-0 text-ink-faint" />
          ) : (
            <ChevronDown size={12} className="shrink-0 text-ink-faint" />
          )}
          <FolderMinus size={12} className="shrink-0 text-ink-faint" />
          <span className="flex-1 truncate italic text-ink-soft">Без проекта</span>
          <span className="text-xs text-ink-faint">· {count}</span>
        </button>
        <button
          type="button"
          onClick={handleCreate}
          disabled={createConversation.isPending}
          aria-label="Новый чат без проекта"
          title="Новый чат без проекта"
          className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-ink-soft opacity-0 transition-opacity hover:bg-paper-sunken hover:text-ink focus-visible:opacity-100 group-hover/bucket:opacity-100 md:h-8 md:w-8"
        >
          <Plus size={14} />
        </button>
      </div>

      {!collapsed && visible.length === 0 && (
        // Empty-state: NOT a listbox (a listbox MUST contain options —
        // otherwise axe flags `aria-required-children` (critical)).
        <p className="ml-5 mt-0.5 px-2 py-1 text-xs italic text-ink-faint">Чаты без проекта</p>
      )}
      {!collapsed && visible.length > 0 && (
        <div
          ref={containerRef as RefObject<HTMLDivElement>}
          onKeyDown={onKeyDown}
          role="listbox"
          aria-label="Чаты без проекта"
          className="ml-5 mt-0.5 space-y-0.5"
        >
          {visible.map((conv, i) => {
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
                      ? 'bg-paper-sunken text-ink'
                      : 'text-ink-soft hover:bg-paper-sunken hover:text-ink'
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
                <ChatRowMenu
                  conversation={conv}
                  pinned={pinned}
                  trigger={
                    <button
                      type="button"
                      aria-label={`Меню чата «${conv.title || 'Новый диалог'}»`}
                      className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md text-ink-soft opacity-0 transition-opacity hover:bg-paper-sunken hover:text-ink focus-visible:opacity-100 group-hover/row:opacity-100"
                    >
                      <MoreHorizontal size={12} />
                    </button>
                  }
                />
              </div>
            );
          })}
          {count > MAX_VISIBLE && (
            <p className="px-2 py-1 text-xs text-ink-faint">…и ещё {count - MAX_VISIBLE}</p>
          )}
        </div>
      )}
    </div>
  );
}
