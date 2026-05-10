'use client';

import { useRouter } from 'next/navigation';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';
import { PageHeader } from '@/components/ui/page-header';
import { ProjectForm } from '@/components/projects/ProjectForm';
import type { Project } from '@/types/project';

export default function NewProjectPage() {
  const router = useRouter();
  const tNew = useTranslations('projects.newPage');

  return (
    <>
      <PageHeader title={tNew('title')} sub={tNew('sub')} />
      <div className="mx-auto w-full max-w-2xl px-4 pb-10 sm:px-12 sm:pb-16">
        <section className="rounded-lg border border-line bg-paper-raised p-5 sm:p-6">
          <ProjectForm
            onSaved={(saved: Project) => {
              toast.success(tNew('createdToast', { name: saved.name }), {
                description: tNew('createdDescription'),
              });
              router.push(`/projects/${saved.id}`);
            }}
          />
        </section>
      </div>
    </>
  );
}
