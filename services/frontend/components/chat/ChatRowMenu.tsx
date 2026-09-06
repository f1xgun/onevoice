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
} from '@/components/design-system/AppAlertDialog';
import {
  Dialog,
  AppDialog as DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/design-system/AppDialog';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { AppInput as Input } from '@/components/design-system/AppInput';
import { MoveChatMenuItem } from '@/components/chat/MoveChatMenuItem';
import { PinChatMenuItem } from '@/components/chat/PinChatMenuItem';
import {
  useDeleteConversation,
  useRegenerateConversationTitle,
  useRenameConversation,
} from '@/hooks/useConversations';
import { usePermission } from '@/lib/hooks/usePermission';
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

  const canUpdate = usePermission('content.update').allowed;
  const canDelete = usePermission('content.delete').allowed;

  const canRegenerate = canUpdate && conversation.titleStatus !== 'manual';
  if (!canUpdate && !canDelete) return null;

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
        onError: () => toast.error(tRow('renameError')),
      }
    );
  }

  function handleRegenerate(e: Event) {
    e.preventDefault();
    regenerateMutation.mutate(conversation.id, {
      onError: (err) => {
        const axErr = err as AxiosError<{ message?: string }> | undefined;
        const msg = axErr?.response?.data?.message ?? tCommon('connectionError');
        toast.error(msg);
      },
    });
  }

  function handleDelete() {
    deleteMutation.mutate(conversation.id, {
      onSuccess: () => {
        setConfirmDeleteOpen(false);
        if (pathname === `/chat/${conversation.id}`) {
          router.push('/chat');
        }
        onDeleted?.();
      },
      onError: () => toast.error(tRow('deleteError')),
    });
  }

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>{trigger}</DropdownMenuTrigger>
        <DropdownMenuContent align={align}>
          {canUpdate && (
            <DropdownMenuItem onSelect={openRename}>
              <Pencil size={14} className="mr-2" />
              {tRow('rename')}
            </DropdownMenuItem>
          )}
          {canRegenerate && (
            <DropdownMenuItem onSelect={handleRegenerate}>
              <RefreshCw size={14} className="mr-2" />
              {tRow('regenerateTitle')}
            </DropdownMenuItem>
          )}
          {canUpdate && (
            <>
              <DropdownMenuSeparator />
              <PinChatMenuItem conversationId={conversation.id} pinned={pinned} />
              <MoveChatMenuItem
                conversationId={conversation.id}
                currentProjectId={conversation.projectId ?? null}
              />
            </>
          )}
          {canDelete && (
            <>
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
            </>
          )}
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
              variant="danger"
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
