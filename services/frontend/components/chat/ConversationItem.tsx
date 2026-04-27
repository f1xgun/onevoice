'use client';

import { useState, useRef, useEffect } from 'react';
import { formatDistanceToNow } from 'date-fns';
import { ru } from 'date-fns/locale';
import { MessageCircle, MoreHorizontal, Pencil, RefreshCw, Trash2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { MoveChatMenuItem } from '@/components/chat/MoveChatMenuItem';

export interface Conversation {
  id: string;
  title: string;
  titleStatus?: 'auto_pending' | 'auto' | 'manual';
  createdAt: string;
  projectId?: string | null;
}

export function ConversationItem({
  conv,
  onOpen,
  onRename,
  onDelete,
  onRegenerateTitle,
}: {
  conv: Conversation;
  onOpen: () => void;
  onRename: (title: string) => void;
  onDelete: () => void;
  onRegenerateTitle: () => void;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(conv.title);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (editing) {
      inputRef.current?.focus();
      inputRef.current?.select();
    }
  }, [editing]);

  const commitRename = () => {
    const trimmed = draft.trim();
    if (trimmed && trimmed !== conv.title) {
      onRename(trimmed);
    } else {
      setDraft(conv.title);
    }
    setEditing(false);
  };

  // Phase 18 / TITLE-01 / D-09 (verbatim): placeholder when title is empty
  // OR an auto-title job is in flight. NO shimmer / skeleton / animation —
  // CONTEXT.md "Sidebar Pending UX" pins the literal Russian copy.
  const displayTitle =
    conv.title === '' || conv.titleStatus === 'auto_pending' ? 'Новый диалог' : conv.title;

  return (
    <div className="group flex items-center gap-3 rounded-lg border border-gray-200 p-4 transition-colors hover:bg-gray-50">
      <MessageCircle size={20} className="shrink-0 text-gray-400" />

      <div className="min-w-0 flex-1">
        {editing ? (
          <Input
            ref={inputRef}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onBlur={commitRename}
            onKeyDown={(e) => {
              if (e.key === 'Enter') commitRename();
              if (e.key === 'Escape') {
                setDraft(conv.title);
                setEditing(false);
              }
            }}
            className="h-7 px-1 py-0 text-sm font-medium"
            onClick={(e) => e.stopPropagation()}
          />
        ) : (
          <button type="button" className="block w-full text-left" onClick={onOpen}>
            <p className="truncate font-medium">{displayTitle}</p>
            <p className="text-sm text-gray-400">
              {formatDistanceToNow(new Date(conv.createdAt), {
                addSuffix: true,
                locale: ru,
              })}
            </p>
          </button>
        )}
      </div>

      {!editing && (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              variant="ghost"
              size="icon"
              className="h-8 w-8 shrink-0 opacity-0 transition-opacity group-hover:opacity-100"
              onClick={(e) => e.stopPropagation()}
            >
              <MoreHorizontal size={16} />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem
              onClick={(e) => {
                e.stopPropagation();
                setDraft(conv.title);
                setEditing(true);
              }}
            >
              <Pencil size={14} className="mr-2" />
              Переименовать
            </DropdownMenuItem>
            {/* Phase 18 / TITLE-09 / D-12: between Переименовать and Удалить.
                Hidden when titleStatus === 'manual' so manual renames stay
                sovereign (D-02 hard rule). */}
            {conv.titleStatus !== 'manual' && (
              <DropdownMenuItem
                onClick={(e) => {
                  e.stopPropagation();
                  onRegenerateTitle();
                }}
              >
                <RefreshCw size={14} className="mr-2" />
                Обновить заголовок
              </DropdownMenuItem>
            )}
            <DropdownMenuSeparator />
            <MoveChatMenuItem conversationId={conv.id} currentProjectId={conv.projectId ?? null} />
            <DropdownMenuSeparator />
            <DropdownMenuItem
              className="text-red-600 focus:text-red-600"
              onClick={(e) => {
                e.stopPropagation();
                onDelete();
              }}
            >
              <Trash2 size={14} className="mr-2" />
              Удалить
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      )}
    </div>
  );
}
