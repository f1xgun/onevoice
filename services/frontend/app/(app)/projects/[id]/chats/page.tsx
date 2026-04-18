'use client';

import { useParams, useRouter } from 'next/navigation';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { MessageCircle, Plus } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { api } from '@/lib/api';
import { useProjectConversationCount, useProjectQuery } from '@/hooks/useProjects';

interface Conversation {
  id: string;
  title: string;
  createdAt: string;
}

export default function ProjectChatsPage() {
  const router = useRouter();
  const params = useParams<{ id: string }>();
  const id = params?.id ?? '';
  const queryClient = useQueryClient();

  const { data: project, isLoading: projectLoading } = useProjectQuery(id);
  const { data: chatCount = 0, isLoading: countLoading } = useProjectConversationCount(id);

  const createConversation = useMutation({
    mutationFn: () =>
      api
        .post('/conversations', { title: 'Новый диалог', projectId: id })
        .then((r) => r.data as Conversation),
    onSuccess: (conv) => {
      void queryClient.invalidateQueries({ queryKey: ['conversations'] });
      void queryClient.invalidateQueries({ queryKey: ['projects', id, 'conversation-count'] });
      router.push(`/chat/${conv.id}`);
    },
  });

  if (projectLoading || countLoading) {
    return (
      <div className="mx-auto w-full max-w-2xl space-y-4 p-6">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-16 w-full" />
        <Skeleton className="h-16 w-full" />
      </div>
    );
  }

  if (!project) {
    return (
      <div className="mx-auto w-full max-w-2xl p-6">
        <p className="text-destructive">Не удалось загрузить проект.</p>
      </div>
    );
  }

  return (
    <div className="mx-auto w-full max-w-2xl p-6">
      <div className="mb-6 flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">{project.name}</h1>
          {project.description && (
            <p className="mt-1 text-sm text-muted-foreground">{project.description}</p>
          )}
        </div>
        <Button
          onClick={() => createConversation.mutate()}
          disabled={createConversation.isPending}
          variant="secondary"
        >
          <Plus size={16} className="mr-2" />
          Новый чат
        </Button>
      </div>

      {chatCount === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-20 text-center">
          <MessageCircle size={48} className="mb-4 text-muted-foreground opacity-40" />
          <p className="text-lg font-medium">В этом проекте ещё нет чатов</p>
          <p className="mt-2 max-w-md text-sm text-muted-foreground">
            Начните первый диалог — он унаследует системный промпт и whitelist этого проекта.
          </p>
          <Button
            className="mt-6"
            variant="secondary"
            onClick={() => createConversation.mutate()}
            disabled={createConversation.isPending}
          >
            <Plus size={16} className="mr-2" />
            Новый чат
          </Button>
        </div>
      ) : (
        // TODO(Phase 19): full chat list here — Plan 15-06 supplies the sidebar chat list.
        <div className="rounded-md border p-4 text-sm text-muted-foreground">
          В проекте {chatCount} чат(ов). Откройте чат через боковую панель.
        </div>
      )}
    </div>
  );
}
