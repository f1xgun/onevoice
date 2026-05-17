'use client';

import { useMemo, useState } from 'react';
import { useForm, type UseFormReturn } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { useQuery } from '@tanstack/react-query';
import { useRouter } from 'next/navigation';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';
import { z } from 'zod';
import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { useBusinessStore } from '@/lib/stores/business';
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

// Schema factory — request-scoped (Phase B1). Called inside the hook
// body via useMemo so a locale switch swaps the validation copy.
function createProjectFormSchema(
  tValidation: (key: string, params?: Record<string, unknown>) => string
) {
  return z
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
}

export type FormValues = z.infer<ReturnType<typeof createProjectFormSchema>>;

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
  const tValidation = useTranslations('validation');
  // Memoize on translator identity (B1). react-hook-form passes the
  // resolver reference into its internal cache, so rebuilding the schema
  // on every render would defeat that cache; rebuilding ONLY on a
  // locale-driven translator swap is the right granularity.
  const schema = useMemo(() => createProjectFormSchema(tValidation), [tValidation]);
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

  // RBAC (plan 02-09): integrations are scoped per business. Switching the
  // active business must surface a fresh list, hence the per-bizId query
  // key and the `enabled` gate.
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const { data: integrations = [] } = useQuery<Integration[]>({
    queryKey: QUERY_KEYS.BUSINESS_INTEGRATIONS(activeBusinessId),
    queryFn: () =>
      bizApi(activeBusinessId!)
        .get<Integration[]>(BIZ_API_PATHS.INTEGRATIONS.ROOT)
        .then((r) => (Array.isArray(r.data) ? r.data : [])),
    enabled: !!activeBusinessId,
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
