'use client';

import { useEffect, useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { api } from '@/lib/api';
import { PLATFORM_FULL_LABELS } from '@/lib/platforms';
import { useTools, groupByPlatform, type PlatformKey } from '@/lib/hooks/useTools';
import {
  useBusinessToolApprovals,
  useUpdateBusinessToolApprovals,
} from '@/lib/hooks/useBusinessToolApprovals';
import type { Business } from '@/types/business';
import type { Tool, ToolApprovalValue, ToolApprovals } from '@/lib/schemas';
import { ToolApprovalToggle } from './ToolApprovalToggle';

// Display order matches the Phase 15 bucket layout so Russian UI screenshots
// stay pixel-stable across phases.
const PLATFORM_DISPLAY_ORDER: PlatformKey[] = [
  'telegram',
  'vk',
  'yandex_business',
  'google_business',
];

// buildDraftFromApprovals hydrates the editor state for every manual-floor
// tool. Backend semantics: when the business hasn't set an entry yet, the
// effective policy defaults to "manual" (POLICY-05, 16-07). We mirror that
// default here so the UI starts in the safe state.
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
    queryKey: ['business'],
    queryFn: () => api.get('/business').then((r) => r.data as Business),
  });

  const { data: tools, isLoading: toolsLoading, error: toolsError } = useTools();

  const businessId = business?.id ?? '';
  const {
    data: savedApprovals,
    isLoading: approvalsLoading,
    error: approvalsError,
  } = useBusinessToolApprovals(businessId);

  const updateMutation = useUpdateBusinessToolApprovals(businessId);

  // The UI shows every manual-floor tool as a switch, plus every
  // forbidden-floor tool as a read-only row. Auto-floor tools bypass HITL
  // entirely and have nothing to configure.
  const manualTools = useMemo<Tool[]>(
    () => (tools ?? []).filter((t) => t.floor === 'manual'),
    [tools]
  );

  const initialDraft = useMemo(
    () => buildDraftFromManualTools(manualTools, savedApprovals ?? {}),
    [manualTools, savedApprovals]
  );

  const [draft, setDraft] = useState<Record<string, ToolApprovalValue>>(initialDraft);

  // Re-seed the editor state whenever the server snapshot changes (initial
  // load, successful Save, concurrent update elsewhere). React Query returns
  // a stable reference per snapshot so this is cheap.
  useEffect(() => {
    setDraft(initialDraft);
  }, [initialDraft]);

  const buckets = useMemo(() => groupByPlatform(tools ?? []), [tools]);
  const platforms = PLATFORM_DISPLAY_ORDER.filter((p) =>
    buckets[p].some((t) => t.floor === 'manual' || t.floor === 'forbidden')
  );

  const isLoading = businessLoading || toolsLoading || approvalsLoading;
  const loadError = toolsError || approvalsError;
  const dirty = !sameDraft(draft, initialDraft);

  function updateTool(toolName: string, value: ToolApprovalValue) {
    setDraft((prev) => ({ ...prev, [toolName]: value }));
  }

  function handleSave() {
    // Send the FULL current map — backend replaces the server's
    // tool_approvals blob wholesale (per 16-07 contract). Partial updates
    // are not supported and would accidentally drop entries.
    updateMutation.mutate(draft, {
      onSuccess: () => {
        toast.success('Настройки сохранены');
      },
      onError: (err) => {
        const msg =
          err instanceof Error && 'response' in err
            ? ((err as { response?: { data?: { error?: string } } }).response?.data?.error ?? '')
            : '';
        toast.error('Не удалось сохранить настройки', {
          description: msg || 'Попробуйте ещё раз.',
        });
      },
    });
  }

  return (
    <div className="mx-auto w-full max-w-3xl space-y-6 p-6">
      <div>
        <h1 className="text-2xl font-semibold">«Настройки одобрения инструментов»</h1>
        <p className="mt-2 text-sm text-muted-foreground">
          Определите, какие действия агент выполняет автоматически, а какие требуют вашего
          одобрения. Запрещённые инструменты нельзя включить отсюда.
        </p>
      </div>

      {isLoading && (
        <div className="space-y-3">
          <Skeleton className="h-24 w-full" />
          <Skeleton className="h-24 w-full" />
          <Skeleton className="h-24 w-full" />
        </div>
      )}

      {loadError && !isLoading && (
        <div className="rounded-md border border-destructive/50 bg-destructive/5 p-4 text-sm text-destructive">
          Не удалось загрузить список инструментов. Обновите страницу или попробуйте позже.
        </div>
      )}

      {!isLoading && !loadError && tools && (
        <div className="space-y-4">
          {platforms.length === 0 && (
            <p className="text-sm text-muted-foreground">Нет настраиваемых инструментов.</p>
          )}
          {platforms.map((platform) => {
            const toolsForPlatform = buckets[platform].filter(
              (t) => t.floor === 'manual' || t.floor === 'forbidden'
            );
            if (toolsForPlatform.length === 0) return null;
            const label = PLATFORM_FULL_LABELS[platform] ?? platform;
            return (
              <Card key={platform}>
                <CardHeader>
                  <CardTitle className="text-base">{label}</CardTitle>
                </CardHeader>
                <CardContent className="space-y-2">
                  {toolsForPlatform.map((tool) => (
                    <ToolApprovalToggle
                      key={tool.name}
                      tool={tool}
                      value={draft[tool.name] ?? 'manual'}
                      onChange={(v) => updateTool(tool.name, v)}
                      disabled={updateMutation.isPending}
                    />
                  ))}
                </CardContent>
              </Card>
            );
          })}

          <div className="flex items-center gap-3 pt-2">
            <Button
              type="button"
              onClick={handleSave}
              disabled={!dirty || updateMutation.isPending}
            >
              {updateMutation.isPending ? 'Сохранение…' : 'Сохранить'}
            </Button>
            {dirty && (
              <span className="text-xs text-muted-foreground">Есть несохранённые изменения</span>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
