'use client';

import { useState } from 'react';
import type { RefObject } from 'react';
import Link from 'next/link';
import { Bookmark, ChevronDown, ChevronRight, MoreHorizontal } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { cn } from '@/lib/utils';
import { useRovingTabIndex } from '@/hooks/useRovingTabIndex';
import { ProjectChip } from '@/components/chat/ProjectChip';
import { ChatRowMenu } from '@/components/chat/ChatRowMenu';
import type { Conversation } from '@/lib/conversations';

// PinnedSection.
//
//   Empty pinned section is HIDDEN entirely (no header, no placeholder).
//   Pinned chats render in BOTH PinnedSection AND under their own project;
//   the global pinned row carries a mini <ProjectChip size="xs"> for
//   project affiliation. Chats in «Без проекта» (projectId == null) get
//   NO chip in the pinned row — the chip is meaningful only when there
//   is a real destination project.
//
// The row is a flex container with TWO siblings: a <Link> wrapping the
// chat title and a sibling <ProjectChip size="xs"> (which itself renders
// a <Link> to the project page). Keeping them as siblings (rather than
// nesting the chip inside the row link) avoids the React `<a> in <a>`
// hydration warning and gives users two distinct navigation targets per
// row (chat title → /chat/{id}, project chip → /projects/{id}).
//
// Caller pre-sorts `conversations` by pinnedAt desc (most-recently-pinned
// first). Re-pinning stamps a fresh now-UTC timestamp at the API, so sort
// stability is enforced server-side.
interface Props {
  conversations: Conversation[]; // expected pre-sorted by pinnedAt desc
  projectsById: Record<string, { id: string; name: string }>;
  activeConversationId?: string;
  onNavigate?: () => void;
}

const MAX_VISIBLE = 20;

export function PinnedSection({
  conversations,
  projectsById,
  activeConversationId,
  onNavigate,
}: Props) {
  const tSide = useTranslations('sidebar');
  const tChat = useTranslations('chat');
  const [collapsed, setCollapsed] = useState(false);

  const count = conversations.length;
  const visible = conversations.slice(0, MAX_VISIBLE);

  const { containerRef, onKeyDown } = useRovingTabIndex(visible.length);

  if (conversations.length === 0) return null;

  return (
    <div className="group/pinned">
      <button
        type="button"
        onClick={() => setCollapsed((v) => !v)}
        aria-expanded={!collapsed}
        aria-label={collapsed ? tSide('expandPinned') : tSide('collapsePinned')}
        className="flex w-full items-center gap-1 rounded-md px-2 py-1.5 text-sm text-ink-mid hover:bg-paper-sunken"
      >
        {collapsed ? (
          <ChevronRight size={12} className="shrink-0 text-ink-faint" />
        ) : (
          <ChevronDown size={12} className="shrink-0 text-ink-faint" />
        )}
        <Bookmark size={12} className="shrink-0 text-yellow-400" />
        <span className="flex-1 truncate text-left">{tSide('pinned')}</span>
        <span className="text-xs text-ink-soft">· {count}</span>
      </button>

      {!collapsed && (
        <div
          ref={containerRef as RefObject<HTMLDivElement>}
          onKeyDown={onKeyDown}
          className="ml-5 mt-0.5 space-y-0.5"
        >
          {visible.map((conv, i) => {
            const project = conv.projectId ? projectsById[conv.projectId] : undefined;
            return (
              <div key={conv.id} className="group/row flex items-center gap-1 px-1">
                <Link
                  href={`/chat/${conv.id}`}
                  onClick={onNavigate}
                  data-roving-item
                  tabIndex={i === 0 ? 0 : -1}
                  aria-current={conv.id === activeConversationId ? 'page' : undefined}
                  className={cn(
                    'flex flex-1 items-center gap-1 truncate rounded-md px-2 py-1 text-xs transition-colors',
                    conv.id === activeConversationId
                      ? 'bg-paper-sunken text-ink'
                      : 'text-ink-soft hover:bg-paper-sunken hover:text-ink'
                  )}
                >
                  <span className="flex-1 truncate">{conv.title || tChat('newConversation')}</span>
                </Link>
                {/* Only chats with a real project get the mini chip.
                    Sibling of the row Link (not nested) to avoid <a in a>. */}
                {project && (
                  <ProjectChip projectId={project.id} projectName={project.name} size="xs" />
                )}
                <ChatRowMenu
                  conversation={conv}
                  pinned
                  trigger={
                    <button
                      type="button"
                      aria-label={tSide('chatRowMenuAria', {
                        title: conv.title || tChat('newConversation'),
                      })}
                      className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md text-gray-400 opacity-0 transition-opacity hover:bg-gray-700 hover:text-white focus-visible:opacity-100 group-hover/row:opacity-100"
                    >
                      <MoreHorizontal size={12} />
                    </button>
                  }
                />
              </div>
            );
          })}
          {count > MAX_VISIBLE && (
            <p className="px-2 py-1 text-xs text-ink-faint">
              {tSide('moreCount', { count: count - MAX_VISIBLE })}
            </p>
          )}
        </div>
      )}
    </div>
  );
}
