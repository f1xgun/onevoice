'use client';

import { useRouter } from 'next/navigation';
import { toast } from 'sonner';
import { ProjectForm } from '@/components/projects/ProjectForm';
import type { Project } from '@/types/project';

export default function NewProjectPage() {
  const router = useRouter();

  return (
    <div className="mx-auto w-full max-w-2xl p-6">
      <h1 className="text-2xl font-semibold">Новый проект</h1>
      <p className="mt-2 text-sm text-muted-foreground">
        Настройте системный промпт, инструменты и быстрые действия для группы чатов.
      </p>
      <div className="mt-6">
        <ProjectForm
          onSaved={(saved: Project) => {
            toast.success(`Проект «${saved.name}» создан`);
            router.push('/chat');
          }}
        />
      </div>
    </div>
  );
}
