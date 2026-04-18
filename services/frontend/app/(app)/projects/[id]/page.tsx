'use client';

import { useParams, useRouter } from 'next/navigation';
import { toast } from 'sonner';
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
      <div className="mx-auto w-full max-w-2xl space-y-4 p-6">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-4 w-96" />
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-40 w-full" />
      </div>
    );
  }

  if (error || !project) {
    return (
      <div className="mx-auto w-full max-w-2xl p-6">
        <p className="text-destructive">Не удалось загрузить проект.</p>
      </div>
    );
  }

  return (
    <div className="mx-auto w-full max-w-2xl p-6">
      <h1 className="text-2xl font-semibold">Редактировать проект</h1>
      <p className="mt-2 text-sm text-muted-foreground">
        Изменения применяются к новым сообщениям. Существующие чаты сохраняют свою историю.
      </p>
      <div className="mt-6">
        <ProjectForm
          project={project}
          onSaved={(saved: Project) => {
            toast.success('Проект сохранён');
            router.push(`/projects/${saved.id}/chats`);
          }}
        />
      </div>
    </div>
  );
}
