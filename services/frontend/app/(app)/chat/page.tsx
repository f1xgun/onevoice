'use client';

import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useRouter } from 'next/navigation';
import { MessageCircle, Plus } from 'lucide-react';
import { toast } from 'sonner';
import type { AxiosError } from 'axios';
import { api } from '@/lib/api';
import { trackClick } from '@/lib/telemetry';
import { Button } from '@/components/ui/button';
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
import { ConversationItem, type Conversation } from '@/components/chat/ConversationItem';

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
      trackClick('create_conversation');
      queryClient.invalidateQueries({ queryKey: ['conversations'] });
      router.push(`/chat/${conv.id}`);
    },
  });

  const { mutate: renameConversation } = useMutation({
    mutationFn: ({ id, title }: { id: string; title: string }) =>
      api.put(`/conversations/${id}`, { title }).then((r) => r.data),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['conversations'] }),
  });

  // Phase 18 / TITLE-09 / D-12: kicks off the auto-title goroutine on the API
  // side. 200 → silently invalidates so the new title arrives via React Query;
  // 409 → server-supplied Russian copy (D-02 / D-03 verbatim) surfaced via
  // sonner toast. Network failure → 'Ошибка соединения' fallback.
  const { mutate: regenerateTitle } = useMutation({
    mutationFn: (id: string) =>
      api.post(`/conversations/${id}/regenerate-title`).then((r) => r.data),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['conversations'] }),
    onError: (err: unknown) => {
      const axErr = err as AxiosError<{ message?: string }> | undefined;
      const msg = axErr?.response?.data?.message ?? 'Ошибка соединения';
      toast.error(msg);
    },
  });

  const { mutate: deleteConversation } = useMutation({
    mutationFn: (id: string) => api.delete(`/conversations/${id}`),
    onSuccess: () => {
      trackClick('delete_conversation');
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
              onRegenerateTitle={() => regenerateTitle(conv.id)}
            />
          ))}
        </div>
      )}

      <AlertDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
      >
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
