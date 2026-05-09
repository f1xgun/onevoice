'use client';

import { useState } from 'react';
import { useForm, type UseFormReturn } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { useQuery } from '@tanstack/react-query';
import { useRouter } from 'next/navigation';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';
import { z } from 'zod';
import { getTranslator } from '@/lib/i18n/translator';
import { api } from '@/lib/api';
import { API_PATHS } from '@/lib/constants/apiPaths';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { MAX_QUICK_ACTIONS } from '@/lib/quick-actions';
import {
  useCreateProject,
  useProjectConversationCount,
  useUpdateProject,
  useDeleteProject,
} from '@/hooks/useProjects';
import { useTools } from '@/lib/hooks/useTools';
import { useBusinessToolApprovals } from '@/lib/hooks/useBusinessToolApprovals';
import type { Tool } from '@/lib/schemas';
import type {
  Project,
  ProjectApprovalOverrides as ProjectApprovalOverridesMap,
} from '@/types/project';

export const MAX_SYSTEM_PROMPT_CHARS = 4000;
const PROJECT_NAME_MAX_LEN = 200;
const PROJECT_DESCRIPTION_MAX_LEN = 2000;

// Schema-time messages — module-level translator (no React context at
// declaration time). Same pattern as `lib/schemas.ts`.
const tValidation = getTranslator('validation');

const schema = z
  .object({
    name: z.string().trim().min(1, tValidation('projectNameRequired')).max(PROJECT_NAME_MAX_LEN),
    description: z.string().max(PROJECT_DESCRIPTION_MAX_LEN),
    systemPrompt: z
      .string()
      .max(
        MAX_SYSTEM_PROMPT_CHARS,
        tValidation('projectSystemPromptMax', { max: MAX_SYSTEM_PROMPT_CHARS })
      ),
    whitelistMode: z.enum(['inherit', 'all', 'explicit', 'none']),
    allowedTools: z.array(z.string()),
    // approvalOverrides. Zod-typed as a map of
    // tool-name → "auto"|"manual". Absence = inherit (Overview invariant
    // #8); the UI never produces a key whose value is the string
    // "inherit".
    approvalOverrides: z.record(z.string(), z.enum(['auto', 'manual'])),
    quickActions: z.array(z.string().trim().min(1)).max(MAX_QUICK_ACTIONS),
  })
  .refine((d) => d.whitelistMode !== 'explicit' || d.allowedTools.length > 0, {
    path: ['allowedTools'],
    message: tValidation('projectAllowedToolsRequired'),
  });

export type FormValues = z.infer<typeof schema>;

interface Integration {
  platform: string;
  status: string;
}

export interface UseProjectFormResult {
  form: UseFormReturn<FormValues>;
  isEdit: boolean;
  submitting: boolean;
  systemPromptLen: number;
  overCap: boolean;
  whitelistMode: FormValues['whitelistMode'];
  activePlatforms: string[];
  tools: Tool[] | undefined;
  businessApprovals: Record<string, 'auto' | 'manual'>;
  chatCount: number;
  deleteOpen: boolean;
  setDeleteOpen: (open: boolean) => void;
  isDeletePending: boolean;
  onSubmit: (e?: React.BaseSyntheticEvent) => Promise<void>;
  handleDelete: () => Promise<void>;
}

export function useProjectForm(
  project: Project | undefined,
  onSaved: (saved: Project) => void
): UseProjectFormResult {
  const router = useRouter();
  const tForm = useTranslations('projects.form');
  const isEdit = !!project;

  const form = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: project?.name ?? '',
      description: project?.description ?? '',
      systemPrompt: project?.systemPrompt ?? '',
      whitelistMode: project?.whitelistMode ?? 'inherit',
      allowedTools: project?.allowedTools ?? [],
      approvalOverrides: (project?.approvalOverrides ?? {}) as ProjectApprovalOverridesMap,
      quickActions: project?.quickActions ?? [],
    },
  });

  const whitelistMode = form.watch('whitelistMode');
  const systemPromptLen = form.watch('systemPrompt').length;
  const overCap = systemPromptLen > MAX_SYSTEM_PROMPT_CHARS;

  const { data: integrations = [] } = useQuery<Integration[]>({
    queryKey: QUERY_KEYS.INTEGRATIONS,
    queryFn: () =>
      api.get(API_PATHS.INTEGRATIONS.ROOT).then((r) => (Array.isArray(r.data) ? r.data : [])),
  });
  const activePlatforms = integrations.filter((i) => i.status === 'active').map((i) => i.platform);

  // live registry (overrides section). Business
  // approvals drive the inherit chip; both are loaded in the background
  // and the form renders a loading note in the overrides section until
  // they resolve.
  const { data: tools } = useTools();
  const { data: businessApprovals = {} } = useBusinessToolApprovals(project?.businessId ?? '');

  const createMutation = useCreateProject();
  const updateMutation = useUpdateProject(project?.id ?? '');
  const deleteMutation = useDeleteProject();

  const [deleteOpen, setDeleteOpen] = useState(false);
  const { data: chatCount = 0 } = useProjectConversationCount(project?.id ?? '', deleteOpen);

  const onSubmit = form.handleSubmit(async (values) => {
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
      toast.error(tForm('saveError'), {
        description: tForm('saveErrorRetry', { detail: msg }).trim(),
      });
    }
  });

  const handleDelete = async () => {
    if (!project) return;
    await deleteMutation.mutateAsync(project.id);
    toast.success(tForm('deletedSuccess'));
    router.push('/chat');
  };

  const submitting = form.formState.isSubmitting;

  return {
    form,
    isEdit,
    submitting,
    systemPromptLen,
    overCap,
    whitelistMode,
    activePlatforms,
    tools,
    businessApprovals,
    chatCount,
    deleteOpen,
    setDeleteOpen,
    isDeletePending: deleteMutation.isPending,
    onSubmit,
    handleDelete,
  };
}
