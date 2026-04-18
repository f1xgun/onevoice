'use client';

import { useState } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { useQuery } from '@tanstack/react-query';
import { useRouter } from 'next/navigation';
import { toast } from 'sonner';
import { z } from 'zod';
import { Button } from '@/components/ui/button';
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form';
import { Input } from '@/components/ui/input';
import { Textarea } from '@/components/ui/textarea';
import { cn } from '@/lib/utils';
import { api } from '@/lib/api';
import { MAX_QUICK_ACTIONS } from '@/lib/quick-actions';
import {
  useCreateProject,
  useProjectConversationCount,
  useUpdateProject,
  useDeleteProject,
} from '@/hooks/useProjects';
import { WhitelistRadio } from './WhitelistRadio';
import { ToolCheckboxGrid } from './ToolCheckboxGrid';
import { QuickActionsEditor } from './QuickActionsEditor';
import { DeleteProjectDialog } from './DeleteProjectDialog';
import type { Project } from '@/types/project';

const MAX_SYSTEM_PROMPT_CHARS = 4000;

const schema = z
  .object({
    name: z.string().trim().min(1, 'Укажите название проекта.').max(200),
    description: z.string().max(2000).default(''),
    systemPrompt: z
      .string()
      .max(MAX_SYSTEM_PROMPT_CHARS, 'Системный промпт слишком длинный (максимум 4000 символов).')
      .default(''),
    whitelistMode: z.enum(['inherit', 'all', 'explicit', 'none']),
    allowedTools: z.array(z.string()).default([]),
    quickActions: z.array(z.string().trim().min(1)).max(MAX_QUICK_ACTIONS).default([]),
  })
  .refine((d) => d.whitelistMode !== 'explicit' || d.allowedTools.length > 0, {
    path: ['allowedTools'],
    message: 'Выберите хотя бы один инструмент или переключите режим на «Никаких».',
  });

type FormValues = z.infer<typeof schema>;

interface Integration {
  platform: string;
  status: string;
}

interface ProjectFormProps {
  project?: Project;
  onSaved: (saved: Project) => void;
}

export function ProjectForm({ project, onSaved }: ProjectFormProps) {
  const router = useRouter();
  const isEdit = !!project;

  const form = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: project?.name ?? '',
      description: project?.description ?? '',
      systemPrompt: project?.systemPrompt ?? '',
      whitelistMode: project?.whitelistMode ?? 'inherit',
      allowedTools: project?.allowedTools ?? [],
      quickActions: project?.quickActions ?? [],
    },
  });

  const whitelistMode = form.watch('whitelistMode');
  const systemPromptLen = form.watch('systemPrompt').length;

  const { data: integrations = [] } = useQuery<Integration[]>({
    queryKey: ['integrations'],
    queryFn: () => api.get('/integrations').then((r) => (Array.isArray(r.data) ? r.data : [])),
  });
  const activePlatforms = integrations.filter((i) => i.status === 'active').map((i) => i.platform);

  const createMutation = useCreateProject();
  const updateMutation = useUpdateProject(project?.id ?? '');
  const deleteMutation = useDeleteProject();

  const [deleteOpen, setDeleteOpen] = useState(false);
  const { data: chatCount = 0 } = useProjectConversationCount(project?.id ?? '', deleteOpen);

  const onSubmit = async (values: FormValues) => {
    try {
      if (isEdit && project) {
        const saved = await updateMutation.mutateAsync(values);
        onSaved(saved);
      } else {
        const saved = await createMutation.mutateAsync(values);
        onSaved(saved);
      }
    } catch (err) {
      const msg =
        err instanceof Error && 'response' in err
          ? ((err as { response?: { data?: { error?: string } } }).response?.data?.error ?? '')
          : '';
      toast.error('Не удалось сохранить проект', {
        description: `Попробуйте ещё раз. ${msg}`.trim(),
      });
    }
  };

  const handleDelete = async () => {
    if (!project) return;
    await deleteMutation.mutateAsync(project.id);
    toast.success('Проект удалён');
    router.push('/chat');
  };

  const overCap = systemPromptLen > MAX_SYSTEM_PROMPT_CHARS;
  const submitting = form.formState.isSubmitting;

  return (
    <Form {...form}>
      <form onSubmit={form.handleSubmit(onSubmit)} className="space-y-6">
        <FormField
          control={form.control}
          name="name"
          render={({ field }) => (
            <FormItem>
              <FormLabel>Название</FormLabel>
              <FormControl>
                <Input placeholder="Например: Отзывы" {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name="description"
          render={({ field }) => (
            <FormItem>
              <FormLabel>
                Описание <span className="text-muted-foreground">(необязательно)</span>
              </FormLabel>
              <FormControl>
                <Textarea
                  rows={2}
                  placeholder="Короткое описание — для кого этот проект."
                  {...field}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name="systemPrompt"
          render={({ field }) => (
            <FormItem>
              <FormLabel>Системный промпт</FormLabel>
              <FormControl>
                <Textarea
                  rows={6}
                  placeholder="Опишите роль и ограничения. Добавляется к контексту бизнеса."
                  {...field}
                />
              </FormControl>
              <div className="flex items-center justify-between">
                <FormDescription>
                  Опишите роль и ограничения. Добавляется к контексту бизнеса.
                </FormDescription>
                <span
                  className={cn(
                    'text-xs tabular-nums',
                    overCap ? 'text-destructive' : 'text-muted-foreground'
                  )}
                  aria-live="polite"
                >
                  {systemPromptLen} / 4000 символов
                </span>
              </div>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name="whitelistMode"
          render={({ field }) => (
            <FormItem>
              <FormLabel>Доступные инструменты</FormLabel>
              <FormControl>
                <WhitelistRadio value={field.value} onChange={field.onChange} name={field.name} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        {whitelistMode === 'explicit' && (
          <FormField
            control={form.control}
            name="allowedTools"
            render={({ field }) => (
              <FormItem>
                <FormLabel className="sr-only">Список инструментов</FormLabel>
                <FormControl>
                  <ToolCheckboxGrid
                    activeIntegrations={activePlatforms}
                    value={field.value}
                    onChange={field.onChange}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        )}

        <FormField
          control={form.control}
          name="quickActions"
          render={({ field }) => (
            <FormItem>
              <FormLabel>Быстрые действия</FormLabel>
              <FormControl>
                <QuickActionsEditor value={field.value} onChange={field.onChange} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        <div className="flex flex-wrap items-center gap-3 pt-2">
          <Button type="submit" disabled={submitting}>
            {isEdit ? 'Сохранить' : 'Создать проект'}
          </Button>
          <Button
            type="button"
            variant="outline"
            onClick={() => router.back()}
            disabled={submitting}
          >
            Отмена
          </Button>
          {isEdit && project && (
            <Button
              type="button"
              variant="outline"
              className="ml-auto text-destructive hover:bg-destructive/10 hover:text-destructive"
              onClick={() => setDeleteOpen(true)}
              disabled={submitting || deleteMutation.isPending}
            >
              Удалить проект
            </Button>
          )}
        </div>
      </form>

      {isEdit && project && (
        <DeleteProjectDialog
          open={deleteOpen}
          onOpenChange={setDeleteOpen}
          projectName={project.name}
          chatCount={chatCount}
          onConfirm={handleDelete}
        />
      )}
    </Form>
  );
}
