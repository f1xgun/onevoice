'use client';

import { useRouter } from 'next/navigation';
import { toast } from 'sonner';
import { PageHeader } from '@/components/ui/page-header';
import { ProjectForm } from '@/components/projects/ProjectForm';
import type { Project } from '@/types/project';

export default function NewProjectPage() {
  const router = useRouter();

  return (
    <>
      <PageHeader
        title="Новый проект"
        sub="Дайте проекту название — остальное настроим позже."
      />
      <div className="mx-auto w-full max-w-2xl px-4 pb-10 sm:px-12 sm:pb-16">
        <section className="rounded-lg border border-line bg-paper-raised p-5 sm:p-6">
          <ProjectForm
            onSaved={(saved: Project) => {
              toast.success(`Проект «${saved.name}» создан`, {
                description: 'Теперь можно настроить промпт и инструменты.',
              });
              router.push(`/projects/${saved.id}`);
            }}
          />
        </section>
      </div>
    </>
  );
}
