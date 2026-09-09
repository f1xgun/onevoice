// app/(app)/tasks/page.tsx — OneVoice (Linen) v2
//
// "Что сделано" — a human-language activity feed. The reader is a small
// business owner, not an engineer. They never see raw error strings,
// JSON payloads, log lines, or technical IDs.
//
// Per design_handoff_onevoice 2/mocks/mock-tasks.jsx:
//   • status dot · title · platform · "когда" + status label
//   • for error rows, an inline warning-soft callout below the row with
//     a plain-Russian reason and (where applicable) a Reconnect CTA or
//     navigation to chat.
//
// No expand/collapse, no terminal log, no KV "Подробности". Brand Voice
// Guide §3: failures explain what + why + what-to-do-next, calmly.

'use client';

import { useCallback, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useLocale, useTranslations } from 'next-intl';
import { format } from 'date-fns';
import { ArrowRight, Loader2 } from 'lucide-react';
import Link from 'next/link';
import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { getDateFnsLocale } from '@/lib/dateFnsLocale';
import type { Locale } from '@/lib/i18n/locales';
import { useBusinessStore } from '@/lib/stores/business';
import { usePermission } from '@/lib/hooks/usePermission';
import {
  TASK_STATUS_DOT_CLASSES,
  useTaskStatusLabels,
  type TaskStatus,
} from '@/lib/constants/statuses';
import { CHANNEL_NAMES } from '@/lib/platforms';
import { useTasksStream } from '@/hooks/useTasksStream';

import { explainError } from './explainError';
import type { AgentTask, TaskStreamEvent } from '@/types/task';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { PlatformIcon } from '@/components/integrations/PlatformIcons';
import { PageHeader } from '@/components/ui/page-header';
import { Skeleton } from '@/components/ui/skeleton';
import { EmptyTasks } from '@/components/states';
import { LoadingPlaceholder } from '@/components/states/LoadingPlaceholder';
import { ListLoadError } from '@/components/lists/ListLoadError';
import { cn } from '@/lib/utils';

// Background poll cadence for the tasks list. SSE drives realtime updates;
// the poll is a belt-and-braces refresh in case the stream missed an event
// (e.g. browser put us to sleep). 30 s is fast enough that a missed task
// surfaces within one poll cycle without flooding the backend.
const POLL_INTERVAL_MS = 30_000;
const VERIFICATION_STATUSES = new Set([
  'pending',
  'running',
  'verified',
  'mismatch',
  'unverifiable',
  'error',
  'unsupported',
]);
const VERIFICATION_FIELDS = new Set(['title', 'description', 'website', 'phone', 'schedule']);

// ─── Top-level page ─────────────────────────────────────────────────

export default function TasksPage() {
  const queryClient = useQueryClient();
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const tHeader = useTranslations('tasks');
  const [filter, setFilter] = useState<'all' | TaskStatus>('all');
  const tActions = useTranslations('tasks.actions');
  const tStats = useTranslations('tasks.stats');
  const {
    allowed: canRead,
    isLoading: permissionLoading,
    isError: permissionError,
    refetch: refetchPermission,
  } = usePermission('content.read');

  const {
    data: queriedTasks = [],
    isLoading: tasksLoading,
    isFetching: tasksFetching,
    isError: tasksError,
    refetch: refetchTasks,
  } = useQuery<AgentTask[]>({
    queryKey: QUERY_KEYS.BUSINESS_TASKS(activeBusinessId),
    queryFn: () =>
      bizApi(activeBusinessId!)
        .get(BIZ_API_PATHS.TASKS.ROOT)
        .then((r) => {
          const data = r.data as unknown;
          if (Array.isArray(data)) return data as AgentTask[];
          const list = (data as { tasks?: AgentTask[] } | null)?.tasks;
          return Array.isArray(list) ? list : [];
        }),
    enabled: !!activeBusinessId && canRead,
    refetchInterval: POLL_INTERVAL_MS,
  });
  const tasks = useMemo(() => (canRead ? queriedTasks : []), [canRead, queriedTasks]);
  const isLoading = permissionLoading || (canRead && tasksLoading);
  const isError = permissionError || (canRead && tasksError);

  const onStreamEvent = useCallback(
    (_: TaskStreamEvent) => {
      queryClient.invalidateQueries({ queryKey: QUERY_KEYS.BUSINESS_TASKS(activeBusinessId) });
    },
    [queryClient, activeBusinessId]
  );
  useTasksStream(onStreamEvent);

  const counts = useMemo(() => {
    const c: Record<TaskStatus, number> = { pending: 0, running: 0, done: 0, error: 0 };
    for (const t of tasks) {
      const s = (t.status as TaskStatus) ?? 'pending';
      if (s in c) c[s] += 1;
    }
    return c;
  }, [tasks]);

  const doneCount = counts.done;
  const inFlight = counts.running + counts.pending;
  const needsHelp = counts.error;
  const visibleTasks = tasks.filter((task) =>
    filter === 'all'
      ? true
      : filter === 'running'
        ? ['pending', 'running'].includes(task.status)
        : task.status === filter
  );
  const statsReady = !isLoading && !isError && canRead;
  const filterOptions = [
    ['all', tActions('all')],
    ['error', tStats('awaitingUser')],
    ['running', tStats('inProgress')],
    ['done', tStats('done')],
  ] as const;

  return (
    <div className="min-h-screen bg-paper">
      <PageHeader title={tHeader('title')} sub={tHeader('subtitle')} />

      {/* BigStat tiles per v2 mock */}
      <div className="grid grid-cols-1 gap-3 px-4 pb-6 sm:grid-cols-3 sm:px-12">
        <BigStat
          label={tStats('done')}
          value={statsReady ? doneCount : '—'}
          hint={doneCount === 0 ? tStats('doneEmptyHint') : tStats('doneSummary')}
          tone="default"
          onClick={() => setFilter('done')}
          selected={filter === 'done'}
          disabled={!statsReady}
          actionLabel={tActions('viewCompleted')}
        />
        <BigStat
          label={tStats('inProgress')}
          value={statsReady ? inFlight : '—'}
          hint={inFlight === 0 ? tStats('inProgressNone') : tStats('inProgressSome')}
          tone={inFlight > 0 ? 'accent' : 'default'}
          onClick={() => setFilter('running')}
          selected={filter === 'running'}
          disabled={!statsReady}
          actionLabel={tActions('viewInProgress')}
        />
        <BigStat
          label={tStats('awaitingUser')}
          value={statsReady ? needsHelp : '—'}
          hint={needsHelp === 0 ? tStats('awaitingNone') : tStats('awaitingSome')}
          tone={needsHelp > 0 ? 'warning' : 'default'}
          onClick={() => setFilter('error')}
          selected={filter === 'error'}
          disabled={!statsReady}
          actionLabel={tActions('viewNeedsHelp')}
        />
      </div>

      {/* Task list */}
      <div className="px-4 pb-16 sm:px-12">
        <div
          className="mb-4 flex flex-wrap items-center gap-2"
          aria-label={tActions('filterLabel')}
          role="group"
        >
          {filterOptions.map(([value, label]) => (
            <Button
              key={value}
              variant={filter === value ? 'secondary' : 'ghost'}
              size="sm"
              aria-pressed={filter === value}
              onClick={() => setFilter(value)}
            >
              {label}
            </Button>
          ))}
        </div>
        {isError ? (
          <ListLoadError
            isPending={tasksFetching || permissionLoading}
            onRetry={() => {
              if (permissionError) void refetchPermission();
              else void refetchTasks();
            }}
          />
        ) : isLoading ? (
          <LoadingPlaceholder>
            <TaskListSkeleton />
          </LoadingPlaceholder>
        ) : visibleTasks.length === 0 ? (
          <EmptyTasks onResetFilters={filter === 'all' ? undefined : () => setFilter('all')} />
        ) : (
          <div className="overflow-hidden rounded-md border border-line bg-paper-raised shadow-ov-1">
            {visibleTasks.map((task, idx) => (
              <TaskRow
                key={task.id}
                task={task}
                last={idx === visibleTasks.length - 1}
                canRead={canRead}
              />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

// ─── Single row (no expand/collapse) ────────────────────────────────

function TaskRow({ task, last, canRead }: { task: AgentTask; last: boolean; canRead: boolean }) {
  const tErrors = useTranslations('tasks.errors');
  const tAgentTaskNames = useTranslations('agentTasks.displayName');
  const tVerification = useTranslations('tasks.verification');
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const queryClient = useQueryClient();
  const [rerunFailed, setRerunFailed] = useState(false);
  const rerun = useMutation({
    mutationFn: ({ businessId, taskId }: { businessId: string; taskId: string }) =>
      bizApi(businessId).post(BIZ_API_PATHS.TASKS.RERUN(taskId)),
    onMutate: () => setRerunFailed(false),
    onSuccess: (_data, variables) =>
      queryClient.invalidateQueries({
        queryKey: QUERY_KEYS.BUSINESS_TASKS(variables.businessId),
      }),
    onError: (_error, variables) => {
      if (useBusinessStore.getState().activeBusinessId === variables.businessId) {
        setRerunFailed(true);
      }
    },
  });
  const taskStatusLabels = useTaskStatusLabels();
  const dateFnsLocale = getDateFnsLocale(useLocale() as Locale);
  const status = (task.status as TaskStatus) ?? 'pending';
  const platformName = CHANNEL_NAMES[task.platform as keyof typeof CHANNEL_NAMES] ?? task.platform;
  const titleClass =
    status === 'error'
      ? 'text-sm font-medium text-danger'
      : 'text-sm font-medium text-ink tracking-[-0.005em]';
  const human = status === 'error' ? explainError(task) : null;

  const localizedName = (() => {
    if (!task.displayNameKey) return null;
    const resolved = tAgentTaskNames(task.displayNameKey);
    return resolved && resolved !== task.displayNameKey ? resolved : null;
  })();
  const titleText = localizedName ?? task.displayName ?? task.type;
  const knownVerificationStatus =
    task.verificationStatus && VERIFICATION_STATUSES.has(task.verificationStatus)
      ? task.verificationStatus
      : null;
  const mismatchNames = (task.verificationMismatches ?? []).map((field) =>
    VERIFICATION_FIELDS.has(field) ? tVerification(`fields.${field}`) : tVerification('unknown')
  );
  const canRerun = canRead && task.verificationCanRerun === true;

  return (
    <div className={cn(!last && 'border-b border-line-soft')}>
      <div className="flex items-start gap-3 px-4 py-3 sm:grid sm:grid-cols-[24px_1fr_160px_180px] sm:items-center sm:gap-4 sm:px-5 sm:py-4">
        {/* Status dot — vertically centered on the title line for mobile. */}
        <span
          aria-hidden
          className={cn(
            'mt-1.5 size-2 shrink-0 rounded-full sm:mt-0 sm:justify-self-center',
            TASK_STATUS_DOT_CLASSES[status]
          )}
        />

        {/* Title (+ optional one-line detail).
            On mobile, platform + when fold underneath the title as a single
            meta line so the row never exceeds the viewport width. */}
        <div className="min-w-0 flex-1">
          <div className={titleClass}>{titleText}</div>
          {status === 'done' &&
            typeof task.output === 'string' &&
            task.output.trim().length > 0 && (
              <div className="mt-0.5 truncate text-[13px] text-ink-mid">{task.output}</div>
            )}
          <div className="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-[12px] text-ink-soft sm:hidden">
            <span className="inline-flex items-center gap-1.5">
              <PlatformIcon platform={task.platform} className="size-4" />
              {platformName}
            </span>
            <span aria-hidden>·</span>
            <span>
              {format(new Date(task.createdAt), 'd MMM HH:mm', { locale: dateFnsLocale })}
            </span>
            <span aria-hidden>·</span>
            <span>{taskStatusLabels[status]}</span>
          </div>
        </div>

        {/* Desktop-only: platform column */}
        <span className="hidden items-center gap-2 sm:flex">
          <PlatformIcon platform={task.platform} />
          <span className="text-[13px] text-ink-mid">{platformName}</span>
        </span>

        {/* Desktop-only: when · status label column */}
        <div className="hidden items-center justify-between gap-3 sm:flex">
          <span className="text-[13px] text-ink-mid">
            {format(new Date(task.createdAt), 'd MMM HH:mm', { locale: dateFnsLocale })}
          </span>
          <span className="text-xs text-ink-soft">{taskStatusLabels[status]}</span>
        </div>
      </div>

      {task.verificationStatus && (
        <div className="mx-4 mb-3 flex flex-wrap items-center gap-3 text-xs text-ink-mid sm:ml-[52px]">
          <span>
            {knownVerificationStatus
              ? tVerification(knownVerificationStatus)
              : tVerification('unknown')}
          </span>
          {mismatchNames.length > 0 && (
            <span>{tVerification('mismatches', { fields: mismatchNames.join(', ') })}</span>
          )}
          {task.verificationCheckedAt && (
            <time dateTime={task.verificationCheckedAt}>
              {tVerification('checkedAt', {
                time: format(new Date(task.verificationCheckedAt), 'd MMM HH:mm', {
                  locale: dateFnsLocale,
                }),
              })}
            </time>
          )}
          {canRerun && (
            <Button
              variant="secondary"
              size="sm"
              className="min-h-[44px]"
              disabled={rerun.isPending || !activeBusinessId}
              aria-busy={rerun.isPending}
              onClick={() => {
                if (!rerun.isPending && activeBusinessId) {
                  rerun.mutate({ businessId: activeBusinessId, taskId: task.id });
                }
              }}
            >
              {rerun.isPending && (
                <Loader2
                  aria-hidden
                  className="mr-2 size-4 animate-spin motion-reduce:animate-none"
                />
              )}
              {rerun.isPending ? tVerification('checking') : tVerification('checkAgain')}
            </Button>
          )}
          {rerunFailed && (
            <span role="alert" className="text-danger">
              {tVerification('failed')}
            </span>
          )}
        </div>
      )}

      {/* Inline human-only warning callout — no log, no JSON, no IDs. */}
      {human && (
        <div className="mx-4 mb-4 flex items-start gap-3 rounded-md border border-warning bg-warning-soft px-4 py-3 sm:mx-5 sm:ml-[52px]">
          <span
            aria-hidden
            className="mt-0.5 inline-flex size-5 shrink-0 items-center justify-center rounded-full bg-warning text-[12px] font-semibold text-paper"
          >
            !
          </span>
          <div className="flex-1">
            <p className="text-[14px] leading-relaxed text-warning-ink">
              {tErrors(human.summaryKey)}
            </p>
            {human.cta && (
              <div className="mt-3">
                <Button variant="secondary" size="sm" asChild>
                  <Link href={human.cta.href}>
                    {tErrors(human.cta.labelKey)}
                    <ArrowRight aria-hidden className="ml-2 size-4" />
                  </Link>
                </Button>
              </div>
            )}
            {human.willAutoRetry && (
              <p className="mt-2 text-xs text-ink-soft">{tErrors('autoRetryHint')}</p>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

// ─── BigStat tile ───────────────────────────────────────────────────

interface BigStatProps {
  label: string;
  value: number | string;
  hint: string;
  tone: 'default' | 'accent' | 'warning';
  selected: boolean;
  disabled: boolean;
  actionLabel: string;
  onClick: () => void;
}

function BigStat({
  label,
  value,
  hint,
  tone,
  selected,
  disabled,
  actionLabel,
  onClick,
}: BigStatProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={selected}
      disabled={disabled}
      className={cn(
        'flex flex-col gap-2 rounded-md border bg-paper-raised px-4 py-4 text-left transition-colors hover:bg-paper-sunken focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:pointer-events-none sm:px-6 sm:py-5',
        tone === 'warning' ? 'border-warning' : 'border-line',
        selected && 'ring-2 ring-brand'
      )}
    >
      <span className="text-meta font-medium text-ink-soft">{label}</span>
      <span className="text-price tabular-nums text-ink">{value}</span>
      <span
        aria-hidden={disabled || undefined}
        className={cn('text-meta text-ink-soft', disabled && 'invisible')}
      >
        {hint}
      </span>
      <span className="mt-2 inline-flex items-center gap-2 text-meta font-medium text-brand">
        {actionLabel}
        <ArrowRight aria-hidden className="size-4" />
      </span>
    </button>
  );
}

// ─── Loading skeleton ───────────────────────────────────────────────

const TASK_SKELETON_ROW_COUNT = 6;

function TaskListSkeleton() {
  return (
    <div className="overflow-hidden rounded-md border border-line bg-paper-raised shadow-ov-1">
      {Array.from({ length: TASK_SKELETON_ROW_COUNT }, (_, i) => (
        <div
          key={i}
          className={cn(
            'flex items-center gap-3 px-4 py-3 sm:grid sm:grid-cols-[24px_1fr_160px_180px] sm:gap-4 sm:px-5 sm:py-4',
            i < TASK_SKELETON_ROW_COUNT - 1 && 'border-b border-line-soft'
          )}
        >
          <Skeleton className="size-2 shrink-0 rounded-full sm:justify-self-center" />
          <Skeleton className="h-4 flex-1 sm:w-64 sm:flex-none" />
          <Skeleton className="hidden h-4 w-32 sm:block" />
          <Skeleton className="hidden h-4 w-24 justify-self-end sm:block" />
        </div>
      ))}
    </div>
  );
}
