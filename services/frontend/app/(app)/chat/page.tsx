'use client';

import { useState, useRef, useEffect } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useRouter } from 'next/navigation';
import { formatDistanceToNow } from 'date-fns';
import { ru } from 'date-fns/locale';
import { MessageCircle, MoreHorizontal, Pencil, Plus, Trash2 } from 'lucide-react';
import { api } from '@/lib/api';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
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

interface Conversation {
  id: string;
  title: string;
  createdAt: string;
}

function ConversationItem({
  conv,
  onOpen,
  onRename,
  onDelete,
}: {
  conv: Conversation;
  onOpen: () => void;
  onRename: (title: string) => void;
  onDelete: () => void;
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
          <button
            type="button"
            className="block w-full text-left"
            onClick={onOpen}
          >
            <p className="truncate font-medium">{conv.title}</p>
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

export default function ChatListPage() {
  const router = useRouter();
  const queryClient = useQueryClient();
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null);

  const { data: conversations = [], isLoading } = useQuery<Conversation[]>({
    queryKey: ['conversations'],
    queryFn: () => api.get('/conversations').then((r) => r.data),
  });

  const { mutate: createConversation, isPending } = useMutation({
    mutationFn: () => api.post('/conversations', { title: 'Новый диалог' }).then((r) => r.data),
    onSuccess: (conv: Conversation) => {
      queryClient.invalidateQueries({ queryKey: ['conversations'] });
      router.push(`/chat/${conv.id}`);
    },
  });

  const { mutate: renameConversation } = useMutation({
    mutationFn: ({ id, title }: { id: string; title: string }) =>
      api.put(`/conversations/${id}`, { title }).then((r) => r.data),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['conversations'] }),
  });

  const { mutate: deleteConversation } = useMutation({
    mutationFn: (id: string) => api.delete(`/conversations/${id}`),
    onSuccess: () => {
      setDeleteTarget(null);
      queryClient.invalidateQueries({ queryKey: ['conversations'] });
    },
  });

  return (
    <div className="mx-auto max-w-2xl p-6">
      <div className="mb-6 flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Диалоги</h1>
        <Button onClick={() => createConversation()} disabled={isPending}>
          <Plus size={16} className="mr-2" />
          Новый диалог
        </Button>
      </div>

      {isLoading ? (
        <div className="space-y-2">
          {[1, 2, 3].map((i) => (
            <div key={i} className="h-16 animate-pulse rounded-lg bg-gray-100" />
          ))}
        </div>
      ) : conversations.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-20 text-gray-400">
          <MessageCircle size={48} className="mb-4 opacity-40" />
          <p className="text-lg">Нет диалогов</p>
          <p className="mt-1 text-sm">Начните новый диалог с AI-ассистентом</p>
        </div>
      ) : (
        <div className="space-y-2">
          {conversations.map((conv) => (
            <ConversationItem
              key={conv.id}
              conv={conv}
              onOpen={() => router.push(`/chat/${conv.id}`)}
              onRename={(title) => renameConversation({ id: conv.id, title })}
              onDelete={() => setDeleteTarget(conv.id)}
            />
          ))}
        </div>
      )}

      <AlertDialog open={deleteTarget !== null} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Удалить диалог?</AlertDialogTitle>
            <AlertDialogDescription>
              Это действие нельзя отменить. Все сообщения будут безвозвратно удалены.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Отмена</AlertDialogCancel>
            <AlertDialogAction
              className="bg-red-600 hover:bg-red-700"
              onClick={() => deleteTarget && deleteConversation(deleteTarget)}
            >
              Удалить
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
