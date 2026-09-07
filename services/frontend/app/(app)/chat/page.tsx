'use client';

import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useRouter } from 'next/navigation';
import { useTranslations } from 'next-intl';
import { MessageCircle, Plus } from 'lucide-react';
import { toast } from 'sonner';
import type { AxiosError } from 'axios';
import { usePermission } from '@/lib/hooks/usePermission';
import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { conversationsQueryKey } from '@/hooks/useConversations';
import { listConversations } from '@/lib/conversations';
import { useBusinessStore } from '@/lib/stores/business';
import { trackClick } from '@/lib/telemetry';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
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
import { ConversationItem, type Conversation } from '@/components/chat/ConversationItem';
import { ListLoadError } from '@/components/lists/ListLoadError';
import { SkeletonInbox } from '@/components/states';

export default function ChatListPage() {
  const router = useRouter();
  const queryClient = useQueryClient();
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const tChat = useTranslations('chat');
  const tCommon = useTranslations('common');
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null);
  const canCreate = usePermission('content.create').allowed;

  const {
    data: conversations = [],
    isPending: isLoading,
    isError,
    refetch,
  } = useQuery<Conversation[]>({
    queryKey: conversationsQueryKey(activeBusinessId),
    queryFn: () => listConversations(activeBusinessId!),
    enabled: !!activeBusinessId,
  });

  const { mutate: createConversation, isPending } = useMutation({
    mutationFn: () =>
      bizApi(activeBusinessId!)
        .post<Conversation>(BIZ_API_PATHS.CONVERSATIONS.ROOT, { title: tChat('newConversation') })
        .then((r) => r.data),
    onSuccess: (conv: Conversation) => {
      trackClick('create_conversation');
      queryClient.invalidateQueries({
        queryKey: conversationsQueryKey(activeBusinessId),
      });
      router.push(`/chat/${conv.id}`);
    },
    onError: () => toast.error(tCommon('connectionError')),
  });

  const { mutate: renameConversation } = useMutation({
    mutationFn: ({ id, title }: { id: string; title: string }) =>
      bizApi(activeBusinessId!)
        .put<Conversation>(BIZ_API_PATHS.CONVERSATIONS.BY_ID(id), { title })
        .then((r) => r.data),
    onSuccess: () =>
      queryClient.invalidateQueries({
        queryKey: conversationsQueryKey(activeBusinessId),
      }),
    onError: () => toast.error(tCommon('connectionError')),
  });

  const { mutate: regenerateTitle } = useMutation({
    mutationFn: (id: string) =>
      bizApi(activeBusinessId!)
        .post(BIZ_API_PATHS.CONVERSATIONS.REGENERATE_TITLE(id))
        .then((r) => r.data),
    onSuccess: () =>
      queryClient.invalidateQueries({
        queryKey: conversationsQueryKey(activeBusinessId),
      }),
    onError: (err: unknown) => {
      const axErr = err as AxiosError<{ message?: string }> | undefined;
      const msg = axErr?.response?.data?.message ?? tCommon('connectionError');
      toast.error(msg);
    },
  });

  const { mutate: deleteConversation } = useMutation({
    mutationFn: (id: string) =>
      bizApi(activeBusinessId!).delete(BIZ_API_PATHS.CONVERSATIONS.BY_ID(id)),
    onSuccess: () => {
      trackClick('delete_conversation');
      setDeleteTarget(null);
      queryClient.invalidateQueries({
        queryKey: conversationsQueryKey(activeBusinessId),
      });
    },
    onError: () => toast.error(tCommon('connectionError')),
  });

  return (
    <div className="mx-auto max-w-[1040px] p-4 md:p-8">
      <div className="mb-6 flex flex-wrap items-center justify-between gap-4">
        <h1 className="text-page-title">{tChat('heading')}</h1>
        {canCreate && (
          <Button onClick={() => createConversation()} disabled={isPending}>
            <Plus size={16} className="mr-2" />
            {tChat('newConversation')}
          </Button>
        )}
      </div>

      {isLoading ? (
        <SkeletonInbox rows={3} />
      ) : isError ? (
        <ListLoadError onRetry={() => void refetch()} />
      ) : conversations.length === 0 ? (
        <div
          role="status"
          className="flex flex-col items-center justify-center py-12 text-center text-ink-soft"
        >
          <MessageCircle size={48} className="mb-4" />
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
              variant="danger"
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
