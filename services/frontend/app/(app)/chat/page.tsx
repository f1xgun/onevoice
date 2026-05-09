'use client';

import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useRouter } from 'next/navigation';
import { useTranslations } from 'next-intl';
import { MessageCircle, Plus } from 'lucide-react';
import { toast } from 'sonner';
import type { AxiosError } from 'axios';
import { api } from '@/lib/api';
import { API_PATHS } from '@/lib/constants/apiPaths';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
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
import { SkeletonInbox } from '@/components/states';

export default function ChatListPage() {
  const router = useRouter();
  const queryClient = useQueryClient();
  const tChat = useTranslations('chat');
  const tCommon = useTranslations('common');
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null);

  const { data: conversations = [], isLoading } = useQuery<Conversation[]>({
    queryKey: QUERY_KEYS.CONVERSATIONS,
    queryFn: () => api.get(API_PATHS.CONVERSATIONS.ROOT).then((r) => r.data),
  });

  const { mutate: createConversation, isPending } = useMutation({
    mutationFn: () =>
      api
        .post(API_PATHS.CONVERSATIONS.ROOT, { title: tChat('newConversation') })
        .then((r) => r.data),
    onSuccess: (conv: Conversation) => {
      trackClick('create_conversation');
      queryClient.invalidateQueries({ queryKey: QUERY_KEYS.CONVERSATIONS });
      router.push(`/chat/${conv.id}`);
    },
  });

  const { mutate: renameConversation } = useMutation({
    mutationFn: ({ id, title }: { id: string; title: string }) =>
      api.put(API_PATHS.CONVERSATIONS.BY_ID(id), { title }).then((r) => r.data),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: QUERY_KEYS.CONVERSATIONS }),
  });

  // Kicks off the auto-title goroutine on the API side. 200 → silently
  // invalidates so the new title arrives via React Query; 409 →
  // server-supplied Russian copy surfaced via sonner toast. Network failure
  // → tCommon('connectionError') fallback.
  const { mutate: regenerateTitle } = useMutation({
    mutationFn: (id: string) =>
      api.post(API_PATHS.CONVERSATIONS.REGENERATE_TITLE(id)).then((r) => r.data),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: QUERY_KEYS.CONVERSATIONS }),
    onError: (err: unknown) => {
      const axErr = err as AxiosError<{ message?: string }> | undefined;
      const msg = axErr?.response?.data?.message ?? tCommon('connectionError');
      toast.error(msg);
    },
  });

  const { mutate: deleteConversation } = useMutation({
    mutationFn: (id: string) => api.delete(API_PATHS.CONVERSATIONS.BY_ID(id)),
    onSuccess: () => {
      trackClick('delete_conversation');
      setDeleteTarget(null);
      queryClient.invalidateQueries({ queryKey: QUERY_KEYS.CONVERSATIONS });
    },
  });

  return (
    <div className="mx-auto max-w-2xl p-6">
      <div className="mb-6 flex items-center justify-between">
        <h1 className="text-2xl font-semibold">{tChat('heading')}</h1>
        <Button onClick={() => createConversation()} disabled={isPending}>
          <Plus size={16} className="mr-2" />
          {tChat('newConversation')}
        </Button>
      </div>

      {isLoading ? (
        // Static inbox-style skeleton per Linen loading rule (no shimmer).
        <SkeletonInbox rows={3} />
      ) : conversations.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-20 text-gray-400">
          <MessageCircle size={48} className="mb-4 opacity-40" />
          <p className="text-lg">{tChat('noConversations')}</p>
          <p className="mt-1 text-sm">{tChat('newConversationCta')}</p>
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
            <AlertDialogTitle>{tChat('deleteConversationTitle')}</AlertDialogTitle>
            <AlertDialogDescription>
              {tChat('deleteConversationDescription')}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{tCommon('cancel')}</AlertDialogCancel>
            <AlertDialogAction
              className="hover:bg-[var(--ov-danger)]/90 border-[var(--ov-danger)] bg-[var(--ov-danger)] text-[oklch(0.99_0_0)]"
              onClick={() => deleteTarget && deleteConversation(deleteTarget)}
            >
              {tCommon('delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
