'use client';

import { useState, useRef, useEffect } from 'react';
import { formatDistanceToNow } from 'date-fns';
import { MoreHorizontal, Pencil, RefreshCw, Trash2 } from 'lucide-react';
import { useLocale, useTranslations } from 'next-intl';
import { useConversationDisplayTitle } from '@/hooks/useConversationDisplayTitle';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { getDateFnsLocale } from '@/lib/dateFnsLocale';
import type { Locale } from '@/lib/i18n/locales';
import { AppInput as Input } from '@/components/design-system/AppInput';
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
  const tRow = useTranslations('chat.rowMenu');
  const getDisplayTitle = useConversationDisplayTitle();
  const dateFnsLocale = getDateFnsLocale(useLocale() as Locale);
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

  const displayTitle = getDisplayTitle(conv);

  return (
    <div className="group flex min-h-16 items-center gap-3 border-b border-line px-4 py-3 hover:bg-paper-sunken">
      <div className="min-w-0 flex-1">
        {editing ? (
          <Input
            ref={inputRef}
            aria-label={tRow('rename')}
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
          <button type="button" className="block min-h-11 w-full text-left" onClick={onOpen}>
            <p className="break-words text-action">{displayTitle}</p>
            <p className="text-sm text-ink-soft">
              {tRow('created')}{' '}
              <time dateTime={conv.createdAt}>
                {formatDistanceToNow(new Date(conv.createdAt), {
                  addSuffix: true,
                  locale: dateFnsLocale,
                })}
              </time>
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
              aria-label={tRow('triggerAria', { title: displayTitle })}
              className="shrink-0"
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
              {tRow('rename')}
            </DropdownMenuItem>
            {/* Between Переименовать and Удалить.
                Hidden when titleStatus === 'manual' so manual renames stay
                sovereign (hard rule). */}
            {conv.titleStatus !== 'manual' && (
              <DropdownMenuItem
                onClick={(e) => {
                  e.stopPropagation();
                  onRegenerateTitle();
                }}
              >
                <RefreshCw size={14} className="mr-2" />
                {tRow('regenerateTitle')}
              </DropdownMenuItem>
            )}
            <DropdownMenuSeparator />
            <MoveChatMenuItem conversationId={conv.id} currentProjectId={conv.projectId ?? null} />
            <DropdownMenuSeparator />
            <DropdownMenuItem
              className="text-[var(--ov-danger)] focus:text-[var(--ov-danger)]"
              onClick={(e) => {
                e.stopPropagation();
                onDelete();
              }}
            >
              <Trash2 size={14} className="mr-2" />
              {tRow('delete')}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      )}
    </div>
  );
}
