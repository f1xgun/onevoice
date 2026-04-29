'use client';

import { useParams, useRouter } from 'next/navigation';
import { toast } from 'sonner';
import { PageHeader } from '@/components/ui/page-header';
import { Skeleton } from '@/components/ui/skeleton';
import { ProjectForm } from '@/components/projects/ProjectForm';
import { useProjectQuery } from '@/hooks/useProjects';
import type { Project } from '@/types/project';

export default function EditProjectPage() {
  const router = useRouter();
  const params = useParams<{ id: string }>();
  const id = params?.id ?? '';
  const { data: project, isLoading, error } = useProjectQuery(id);

  if (isLoading) {
    return (
      <>
        <PageHeader title="Редактировать проект" />
        <div className="mx-auto w-full max-w-2xl space-y-3 px-4 pb-10 sm:px-12 sm:pb-16">
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-24 w-full" />
          <Skeleton className="h-40 w-full" />
        </div>
      </>
    );
  }

  if (error || !project) {
    return (
      <>
        <PageHeader title="Редактировать проект" />
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
        title="Редактировать проект"
        sub="Изменения применяются к новым сообщениям. Существующие чаты сохраняют свою историю."
      />
      <div className="mx-auto w-full max-w-2xl px-4 pb-10 sm:px-12 sm:pb-16">
        <section className="rounded-lg border border-line bg-paper-raised p-5 sm:p-6">
          <ProjectForm
            project={project}
            onSaved={(saved: Project) => {
              toast.success('Проект сохранён');
              router.push(`/projects/${saved.id}/chats`);
            }}
          />
        </section>
      </div>
    </>
  );
}
