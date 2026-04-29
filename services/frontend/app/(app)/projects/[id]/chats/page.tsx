'use client';

import { useParams, useRouter } from 'next/navigation';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Plus } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { PageHeader } from '@/components/ui/page-header';
import { MonoLabel } from '@/components/ui/mono-label';
import { EmptyFrame } from '@/components/states';
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
      <>
        <PageHeader title="Проект" />
        <div className="mx-auto w-full max-w-2xl space-y-3 px-4 pb-10 sm:px-12 sm:pb-16">
          <Skeleton className="h-16 w-full" />
          <Skeleton className="h-16 w-full" />
        </div>
      </>
    );
  }

  if (!project) {
    return (
      <>
        <PageHeader title="Проект" />
        <div className="mx-auto w-full max-w-2xl px-4 pb-10 sm:px-12 sm:pb-16">
          <div className="rounded-lg border border-[var(--ov-danger)]/40 bg-[var(--ov-danger-soft)] p-6 text-sm text-[var(--ov-danger)]">
            Не удалось загрузить проект. Обновите страницу или попробуйте позже.
          </div>
        </div>
      </>
    );
  }

  return (
    <>
      <PageHeader
        title={project.name}
        sub={project.description}
        actions={
          <Button
            variant="primary"
            size="md"
            onClick={() => createConversation.mutate()}
            disabled={createConversation.isPending}
          >
            <Plus size={16} />
            Новый чат
          </Button>
        }
      />

      <div className="mx-auto w-full max-w-2xl px-4 pb-10 sm:px-12 sm:pb-16">
        {chatCount === 0 ? (
          <EmptyFrame
            title="В этом проекте ещё нет чатов"
            body="Начните первый диалог — он унаследует системный промпт и whitelist этого проекта."
            action={
              <Button
                variant="primary"
                size="md"
                onClick={() => createConversation.mutate()}
                disabled={createConversation.isPending}
              >
                <Plus size={16} />
                Новый чат
              </Button>
            }
          />
        ) : (
          // TODO(Phase 19): full chat list here — Plan 15-06 supplies the
          // sidebar chat list. Until then, point the operator at the rail.
          <section className="rounded-lg border border-line bg-paper-raised p-5">
            <MonoLabel>Чаты проекта</MonoLabel>
            <p className="mt-2 text-sm text-ink">
              В этом проекте {chatCount} {pluralizeChats(chatCount)}.
            </p>
            <p className="mt-1 text-sm text-ink-mid">
              Откройте нужный через боковую панель.
            </p>
          </section>
        )}
      </div>
    </>
  );
}

function pluralizeChats(n: number): string {
  const last = n % 10;
  const lastTwo = n % 100;
  if (lastTwo >= 11 && lastTwo <= 14) return 'чатов';
  if (last === 1) return 'чат';
  if (last >= 2 && last <= 4) return 'чата';
  return 'чатов';
}
