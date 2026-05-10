'use client';

import { useState, type ReactNode } from 'react';
import { usePathname, useRouter } from 'next/navigation';
import { Pencil, RefreshCw, Trash2 } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';
import type { AxiosError } from 'axios';

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { MoveChatMenuItem } from '@/components/chat/MoveChatMenuItem';
import { PinChatMenuItem } from '@/components/chat/PinChatMenuItem';
import {
  useDeleteConversation,
  useRegenerateConversationTitle,
  useRenameConversation,
} from '@/hooks/useConversations';
import type { Conversation } from '@/lib/conversations';

interface Props {
  conversation: Pick<Conversation, 'id' | 'title' | 'titleStatus' | 'projectId'>;
  // Derived once at the call site (typically conv.pinnedAt != null) so
  // ChatHeader can pass its own narrow primitive selector value without
  // having to fabricate a synthetic pinnedAt timestamp.
  pinned: boolean;
  trigger: ReactNode;
  align?: 'start' | 'end' | 'center';
  onDeleted?: () => void;
}

// ChatRowMenu — single source of truth for per-chat actions (rename,
// regenerate title, move, pin/unpin, delete). Used from sidebar rows
// (ProjectSection / UnassignedBucket / PinnedSection) and ChatHeader so
// every chat-context entry point exposes the same menu.
//
// Rename surfaces as a Dialog with an Input (a dropdown can't host an
// inline editable field — Radix unmounts on close). Delete surfaces as
// AlertDialog with the locked Russian copy from the legacy /chat list
// page so the destructive-confirmation contract stays identical.
//
// Regenerate-title 409 toasts the server-supplied verbatim Russian
// message (see RegenerateMenuItem.test.tsx).
export function ChatRowMenu({ conversation, pinned, trigger, align = 'end', onDeleted }: Props) {
  const tRow = useTranslations('chat.rowMenu');
  const tCommon = useTranslations('common');
  const tChat = useTranslations('chat');
  const router = useRouter();
  const pathname = usePathname();
  const [renameOpen, setRenameOpen] = useState(false);
  const [renameDraft, setRenameDraft] = useState(conversation.title);
  const [confirmDeleteOpen, setConfirmDeleteOpen] = useState(false);

  const renameMutation = useRenameConversation();
  const regenerateMutation = useRegenerateConversationTitle();
  const deleteMutation = useDeleteConversation();

  const canRegenerate = conversation.titleStatus !== 'manual';

  function openRename(e: Event) {
    e.preventDefault();
    setRenameDraft(conversation.title);
    setRenameOpen(true);
  }

  function commitRename() {
    const trimmed = renameDraft.trim();
    if (!trimmed || trimmed === conversation.title) {
      setRenameOpen(false);
      return;
    }
    renameMutation.mutate(
      { id: conversation.id, title: trimmed },
      {
        onSuccess: () => setRenameOpen(false),
        onError: () => toast.error('Не удалось переименовать чат'),
      }
    );
  }

  function handleRegenerate(e: Event) {
    e.preventDefault();
    regenerateMutation.mutate(conversation.id, {
      onError: (err) => {
        const axErr = err as AxiosError<{ message?: string }> | undefined;
        const msg = axErr?.response?.data?.message ?? 'Ошибка соединения';
        toast.error(msg);
      },
    });
  }

  function handleDelete() {
    deleteMutation.mutate(conversation.id, {
      onSuccess: () => {
        setConfirmDeleteOpen(false);
        // If the user is currently viewing the chat we just deleted, redirect
        // off the now-404 URL. Triggered for ANY entry point (sidebar row +
        // header menu) so deleting from the sidebar while the chat is open
        // doesn't leave the user staring at a broken /chat/<id> page.
        if (pathname === `/chat/${conversation.id}`) {
          router.push('/chat');
        }
        onDeleted?.();
      },
      onError: () => toast.error('Не удалось удалить чат'),
    });
  }

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>{trigger}</DropdownMenuTrigger>
        <DropdownMenuContent align={align}>
          <DropdownMenuItem onSelect={openRename}>
            <Pencil size={14} className="mr-2" />
            {tRow('rename')}
          </DropdownMenuItem>
          {canRegenerate && (
            <DropdownMenuItem onSelect={handleRegenerate}>
              <RefreshCw size={14} className="mr-2" />
              {tRow('regenerateTitle')}
            </DropdownMenuItem>
          )}
          <DropdownMenuSeparator />
          <PinChatMenuItem conversationId={conversation.id} pinned={pinned} />
          <MoveChatMenuItem
            conversationId={conversation.id}
            currentProjectId={conversation.projectId ?? null}
          />
          <DropdownMenuSeparator />
          <DropdownMenuItem
            className="text-red-600 focus:text-red-600"
            onSelect={(e) => {
              e.preventDefault();
              setConfirmDeleteOpen(true);
            }}
          >
            <Trash2 size={14} className="mr-2" />
            {tRow('delete')}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      <Dialog open={renameOpen} onOpenChange={setRenameOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{tRow('renameDialogTitle')}</DialogTitle>
            <DialogDescription>{tRow('renameDialogDescription')}</DialogDescription>
          </DialogHeader>
          <Input
            value={renameDraft}
            onChange={(e) => setRenameDraft(e.target.value)}
            placeholder={tRow('renamePlaceholder')}
            maxLength={200}
            autoFocus
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault();
                commitRename();
              }
            }}
          />
          <DialogFooter>
            <Button variant="outline" onClick={() => setRenameOpen(false)}>
              {tCommon('cancel')}
            </Button>
            <Button onClick={commitRename} disabled={renameMutation.isPending}>
              {tCommon('save')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={confirmDeleteOpen} onOpenChange={setConfirmDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{tChat('deleteConversationTitle')}</AlertDialogTitle>
            <AlertDialogDescription>
              {tChat('deleteConversationDescription')}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{tCommon('cancel')}</AlertDialogCancel>
            <AlertDialogAction
              className="bg-red-600 hover:bg-red-700"
              onClick={handleDelete}
              disabled={deleteMutation.isPending}
            >
              {tCommon('delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
