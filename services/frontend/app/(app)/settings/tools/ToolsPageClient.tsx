'use client';

import { useEffect, useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { PageHeader } from '@/components/ui/page-header';
import { MonoLabel } from '@/components/ui/mono-label';
import { api } from '@/lib/api';
import { API_PATHS } from '@/lib/constants/apiPaths';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { PLATFORM_FULL_LABELS } from '@/lib/platforms';
import { useTools, groupByPlatform, TOOL_PLATFORM_ORDER } from '@/lib/hooks/useTools';
import { RU_PLURAL_PAUCAL_UPPER, RU_PLURAL_TEEN_LOWER, RU_PLURAL_TEEN_UPPER } from '@/lib/plural';
import {
  useBusinessToolApprovals,
  useUpdateBusinessToolApprovals,
} from '@/lib/hooks/useBusinessToolApprovals';
import type { Business } from '@/types/business';
import type { Tool, ToolApprovalValue, ToolApprovals } from '@/lib/schemas';
import { ToolApprovalToggle } from './ToolApprovalToggle';

function buildDraftFromManualTools(
  manualTools: Tool[],
  saved: ToolApprovals
): Record<string, ToolApprovalValue> {
  const draft: Record<string, ToolApprovalValue> = {};
  for (const tool of manualTools) {
    const existing = saved[tool.name];
    draft[tool.name] = existing === 'auto' ? 'auto' : 'manual';
  }
  return draft;
}

function sameDraft(
  a: Record<string, ToolApprovalValue>,
  b: Record<string, ToolApprovalValue>
): boolean {
  const keysA = Object.keys(a);
  const keysB = Object.keys(b);
  if (keysA.length !== keysB.length) return false;
  for (const key of keysA) {
    if (a[key] !== b[key]) return false;
  }
  return true;
}

export function ToolsPageClient() {
  const { data: business, isLoading: businessLoading } = useQuery<Business>({
    queryKey: QUERY_KEYS.BUSINESS,
    queryFn: () => api.get(API_PATHS.BUSINESS.ROOT).then((r) => r.data as Business),
  });

  const { data: tools, isLoading: toolsLoading, error: toolsError } = useTools();

  const businessId = business?.id ?? '';
  const {
    data: savedApprovals,
    isLoading: approvalsLoading,
    error: approvalsError,
  } = useBusinessToolApprovals(businessId);

  const updateMutation = useUpdateBusinessToolApprovals(businessId);

  const manualTools = useMemo<Tool[]>(
    () => (tools ?? []).filter((t) => t.floor === 'manual'),
    [tools]
  );

  const initialDraft = useMemo(
    () => buildDraftFromManualTools(manualTools, savedApprovals ?? {}),
    [manualTools, savedApprovals]
  );

  const [draft, setDraft] = useState<Record<string, ToolApprovalValue>>(initialDraft);

  useEffect(() => {
    setDraft(initialDraft);
  }, [initialDraft]);

  const buckets = useMemo(() => groupByPlatform(tools ?? []), [tools]);
  const platforms = TOOL_PLATFORM_ORDER.filter((p) =>
    buckets[p].some((t) => t.floor === 'manual' || t.floor === 'forbidden')
  );

  const isLoading = businessLoading || toolsLoading || approvalsLoading;
  const loadError = toolsError || approvalsError;
  const dirty = !sameDraft(draft, initialDraft);

  function updateTool(toolName: string, value: ToolApprovalValue) {
    setDraft((prev) => ({ ...prev, [toolName]: value }));
  }

  function handleSave() {
    updateMutation.mutate(draft, {
      onSuccess: () => {
        toast.success('Настройки сохранены');
      },
      onError: (err) => {
        const msg =
          err instanceof Error && 'response' in err
            ? ((err as { response?: { data?: { error?: string } } }).response?.data?.error ?? '')
            : '';
        toast.error('Не удалось сохранить', {
          description: msg || 'Попробуйте ещё раз.',
        });
      },
    });
  }

  return (
    <>
      <PageHeader
        title="Что разрешено ИИ"
        sub="OneVoice выполняет одни действия сам, для других сначала спрашивает вас. Запрещённые инструменты включить отсюда нельзя."
        actions={
          <Button type="button" onClick={handleSave} disabled={!dirty || updateMutation.isPending}>
            {updateMutation.isPending ? 'Сохраняем…' : 'Сохранить'}
          </Button>
        }
      />

      <div className="mx-auto flex w-full max-w-[860px] flex-col gap-6 px-4 pb-10 sm:px-12 sm:pb-16">
        {dirty && (
          <div className="rounded-md border border-line bg-paper-raised px-4 py-3 text-xs text-ink-mid">
            Есть несохранённые изменения. Нажмите «Сохранить», чтобы применить.
          </div>
        )}

        {isLoading && (
          <div className="flex flex-col gap-3">
            <Skeleton className="h-24 w-full" />
            <Skeleton className="h-24 w-full" />
            <Skeleton className="h-24 w-full" />
          </div>
        )}

        {loadError && !isLoading && (
          <div className="rounded-md border border-[oklch(0.85_0.08_25)] bg-[var(--ov-danger-soft)] p-4 text-sm text-[var(--ov-danger)]">
            Не удалось загрузить список инструментов. Обновите страницу или попробуйте позже.
          </div>
        )}

        {!isLoading && !loadError && tools && (
          <>
            {platforms.length === 0 && (
              <p className="text-sm text-ink-mid">Нет настраиваемых инструментов.</p>
            )}
            {platforms.map((platform) => {
              const toolsForPlatform = buckets[platform].filter(
                (t) => t.floor === 'manual' || t.floor === 'forbidden'
              );
              if (toolsForPlatform.length === 0) return null;
              const label = PLATFORM_FULL_LABELS[platform] ?? platform;
              return (
                <section
                  key={platform}
                  className="overflow-hidden rounded-lg border border-line bg-paper-raised"
                >
                  <header className="flex items-center justify-between border-b border-line-soft px-5 py-4">
                    <div>
                      <MonoLabel>Платформа</MonoLabel>
                      <h2 className="mt-1 text-base font-medium text-ink">{label}</h2>
                    </div>
                    <MonoLabel>
                      {toolsForPlatform.length} {pluralizeTools(toolsForPlatform.length)}
                    </MonoLabel>
                  </header>
                  <div className="flex flex-col gap-2 p-4">
                    {toolsForPlatform.map((tool) => (
                      <ToolApprovalToggle
                        key={tool.name}
                        tool={tool}
                        value={draft[tool.name] ?? 'manual'}
                        onChange={(v) => updateTool(tool.name, v)}
                        disabled={updateMutation.isPending}
                      />
                    ))}
                  </div>
                </section>
              );
            })}
          </>
        )}
      </div>
    </>
  );
}

function pluralizeTools(n: number): string {
  const last = n % 10;
  const lastTwo = n % 100;
  if (lastTwo >= RU_PLURAL_TEEN_LOWER && lastTwo <= RU_PLURAL_TEEN_UPPER) return 'инструментов';
  if (last === 1) return 'инструмент';
  if (last >= 2 && last <= RU_PLURAL_PAUCAL_UPPER) return 'инструмента';
  return 'инструментов';
}
