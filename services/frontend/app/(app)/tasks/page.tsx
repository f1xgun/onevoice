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
import {
  ArrowRight,
  CheckCircle2,
  CircleAlert,
  Clock3,
  Loader2,
  LoaderCircle,
  X,
} from 'lucide-react';
import Link from 'next/link';
import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { getDateFnsLocale } from '@/lib/dateFnsLocale';
import type { Locale } from '@/lib/i18n/locales';
import { useBusinessStore } from '@/lib/stores/business';
import { usePermission } from '@/lib/hooks/usePermission';
import { useTaskStatusLabels, type TaskStatus } from '@/lib/constants/statuses';
import { usePlatformFullLabels } from '@/lib/platforms';
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
import { Badge } from '@/components/ui/badge';
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
  const { allowed: canUpdate } = usePermission('content.update');

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
          tone={needsHelp > 0 ? 'danger' : 'default'}
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
                canUpdate={canUpdate}
              />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

// ─── Single row (no expand/collapse) ────────────────────────────────

const RETRYABLE_ERROR_CODES = new Set(['transient', 'rate_limit_exceeded', 'channel_not_found']);

const TASK_STATUS_TONES: Record<TaskStatus, 'neutral' | 'warning' | 'success' | 'danger'> = {
  pending: 'neutral',
  running: 'warning',
  done: 'success',
  error: 'danger',
};

const TASK_STATUS_ROW_CLASSES: Record<TaskStatus, string> = {
  pending: 'border-l-4 border-l-ink-faint',
  running: 'border-l-4 border-l-warning',
  done: 'border-l-4 border-l-success',
  error: 'border-l-4 border-l-danger bg-danger-soft',
};

function TaskStatusIcon({ status }: { status: TaskStatus }) {
  const iconClass = 'size-5';
  if (status === 'done')
    return <CheckCircle2 aria-hidden className={cn(iconClass, 'text-success')} />;
  if (status === 'error')
    return <CircleAlert aria-hidden className={cn(iconClass, 'text-danger')} />;
  if (status === 'running') {
    return (
      <LoaderCircle
        aria-hidden
        className={cn(iconClass, 'animate-spin text-warning motion-reduce:animate-none')}
      />
    );
  }
  return <Clock3 aria-hidden className={cn(iconClass, 'text-ink-soft')} />;
}

function TaskStatusBadge({ status, label }: { status: TaskStatus; label: string }) {
  return (
    <Badge tone={TASK_STATUS_TONES[status]} className="border border-current font-semibold">
      {label}
    </Badge>
  );
}

function TaskRow({
  task,
  last,
  canRead,
  canUpdate,
}: {
  task: AgentTask;
  last: boolean;
  canRead: boolean;
  canUpdate: boolean;
}) {
  const tTasks = useTranslations('tasks');
  const tErrors = useTranslations('tasks.errors');
  const tActions = useTranslations('tasks.actions');
  const tAgentTaskNames = useTranslations('agentTasks.displayName');
  const tVerification = useTranslations('tasks.verification');
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const platformLabels = usePlatformFullLabels();
  const queryClient = useQueryClient();
  const [rerunFailed, setRerunFailed] = useState(false);
  const [retryFailed, setRetryFailed] = useState(false);
  const [dismissFailed, setDismissFailed] = useState(false);
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
  const retry = useMutation({
    mutationFn: ({ businessId, taskId }: { businessId: string; taskId: string }) =>
      bizApi(businessId).post(BIZ_API_PATHS.TASKS.RETRY(taskId)),
    onMutate: () => setRetryFailed(false),
    onSuccess: (_data, variables) =>
      queryClient.invalidateQueries({
        queryKey: QUERY_KEYS.BUSINESS_TASKS(variables.businessId),
      }),
    onError: () => setRetryFailed(true),
  });
  const dismiss = useMutation({
    mutationFn: ({ businessId, taskId }: { businessId: string; taskId: string }) =>
      bizApi(businessId).delete(BIZ_API_PATHS.TASKS.BY_ID(taskId)),
    onMutate: async ({ businessId, taskId }) => {
      setDismissFailed(false);
      const queryKey = QUERY_KEYS.BUSINESS_TASKS(businessId);
      await queryClient.cancelQueries({ queryKey });
      const previous = queryClient.getQueryData<AgentTask[]>(queryKey);
      queryClient.setQueryData<AgentTask[]>(queryKey, (current) =>
        current?.filter((entry) => entry.id !== taskId)
      );
      return { previous, queryKey };
    },
    onError: (_error, _variables, context) => {
      if (context?.previous) queryClient.setQueryData(context.queryKey, context.previous);
      setDismissFailed(true);
    },
    onSettled: (_data, _error, variables) =>
      queryClient.invalidateQueries({
        queryKey: QUERY_KEYS.BUSINESS_TASKS(variables.businessId),
      }),
  });
  const taskStatusLabels = useTaskStatusLabels();
  const dateFnsLocale = getDateFnsLocale(useLocale() as Locale);
  const status = (task.status as TaskStatus) ?? 'pending';
  const platformName = platformLabels[task.platform] ?? tTasks('unknownPlatform');
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
  const titleText = localizedName ?? task.displayName ?? tTasks('unknownAction');
  const knownVerificationStatus =
    task.verificationStatus && VERIFICATION_STATUSES.has(task.verificationStatus)
      ? task.verificationStatus
      : null;
  const mismatchNames = (task.verificationMismatches ?? []).map((field) =>
    VERIFICATION_FIELDS.has(field) ? tVerification(`fields.${field}`) : tVerification('unknown')
  );
  const canRerun = canRead && task.verificationCanRerun === true;
  const canRetry = canUpdate && !!task.errorCode && RETRYABLE_ERROR_CODES.has(task.errorCode);
  const taskActionPending = retry.isPending || dismiss.isPending;

  return (
    <div className={cn(TASK_STATUS_ROW_CLASSES[status], !last && 'border-b border-line-soft')}>
      <div className="flex items-start gap-3 px-4 py-3 lg:grid lg:grid-cols-[32px_minmax(0,1fr)_180px_140px_132px] lg:items-center lg:gap-4 lg:px-5 lg:py-4">
        <span className="mt-0.5 shrink-0 lg:mt-0 lg:justify-self-center">
          <TaskStatusIcon status={status} />
        </span>

        {/* Title (+ optional one-line detail).
            On mobile, platform + when fold underneath the title as a single
            meta line so the row never exceeds the viewport width. */}
        <div className="min-w-0 flex-1">
          <div className={titleClass}>{titleText}</div>
          <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-2 text-[12px] text-ink-soft lg:hidden">
            <span className="inline-flex items-center gap-1.5">
              <PlatformIcon platform={task.platform} className="size-4" />
              {platformName}
            </span>
            <span aria-hidden>·</span>
            <span>
              {format(new Date(task.createdAt), 'd MMM HH:mm', { locale: dateFnsLocale })}
            </span>
            <TaskStatusBadge status={status} label={taskStatusLabels[status]} />
          </div>
        </div>

        {/* Desktop-only: platform column */}
        <span className="hidden min-w-0 items-center gap-2 lg:flex">
          <PlatformIcon platform={task.platform} />
          <span className="truncate text-[13px] text-ink-mid">{platformName}</span>
        </span>

        <time
          className="hidden whitespace-nowrap text-[13px] tabular-nums text-ink-mid lg:block"
          dateTime={task.createdAt}
        >
          {format(new Date(task.createdAt), 'd MMM HH:mm', { locale: dateFnsLocale })}
        </time>

        <span className="hidden lg:block lg:justify-self-start">
          <TaskStatusBadge status={status} label={taskStatusLabels[status]} />
        </span>
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
        <div className="mx-4 mb-4 flex items-start gap-3 rounded-md border border-danger bg-paper-raised px-4 py-3 lg:mx-5 lg:ml-[52px]">
          <span
            aria-hidden
            className="mt-0.5 inline-flex size-5 shrink-0 items-center justify-center rounded-full bg-danger text-[12px] font-semibold text-paper"
          >
            !
          </span>
          <div className="flex-1">
            <p className="text-[14px] leading-relaxed text-ink">{tErrors(human.summaryKey)}</p>
            <div className="mt-3 flex flex-wrap gap-2">
              {human.cta && (
                <Button variant="secondary" size="sm" asChild>
                  <Link href={human.cta.href}>
                    {tErrors(human.cta.labelKey)}
                    <ArrowRight aria-hidden className="ml-2 size-4" />
                  </Link>
                </Button>
              )}
              {canRetry && (
                <Button
                  variant="secondary"
                  size="sm"
                  disabled={taskActionPending || !activeBusinessId}
                  aria-busy={retry.isPending}
                  onClick={() => {
                    if (!taskActionPending && activeBusinessId) {
                      retry.mutate({ businessId: activeBusinessId, taskId: task.id });
                    }
                  }}
                >
                  {retry.isPending && (
                    <Loader2
                      aria-hidden
                      className="mr-2 size-4 animate-spin motion-reduce:animate-none"
                    />
                  )}
                  {retry.isPending ? tActions('retrying') : tActions('retry')}
                </Button>
              )}
              {canUpdate && (
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={taskActionPending || !activeBusinessId}
                  aria-busy={dismiss.isPending}
                  onClick={() => {
                    if (!taskActionPending && activeBusinessId) {
                      dismiss.mutate({ businessId: activeBusinessId, taskId: task.id });
                    }
                  }}
                >
                  {dismiss.isPending ? (
                    <Loader2
                      aria-hidden
                      className="mr-2 size-4 animate-spin motion-reduce:animate-none"
                    />
                  ) : (
                    <X aria-hidden className="mr-2 size-4" />
                  )}
                  {dismiss.isPending ? tActions('removing') : tActions('remove')}
                </Button>
              )}
            </div>
            {(retryFailed || dismissFailed) && (
              <p role="alert" className="mt-2 text-xs text-danger">
                {retryFailed ? tActions('retryFailed') : tActions('removeFailed')}
              </p>
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
  tone: 'default' | 'accent' | 'danger';
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
        tone === 'danger' ? 'border-danger bg-danger-soft' : 'border-line',
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
            'flex items-center gap-3 border-l-4 border-l-ink-faint px-4 py-3 lg:grid lg:grid-cols-[32px_minmax(0,1fr)_180px_140px_132px] lg:gap-4 lg:px-5 lg:py-4',
            i < TASK_SKELETON_ROW_COUNT - 1 && 'border-b border-line-soft'
          )}
        >
          <Skeleton className="size-5 shrink-0 rounded-full lg:justify-self-center" />
          <Skeleton className="h-4 flex-1 lg:w-64 lg:flex-none" />
          <Skeleton className="hidden h-4 w-32 lg:block" />
          <Skeleton className="hidden h-4 w-24 lg:block" />
          <Skeleton className="hidden h-[22px] w-24 rounded-full lg:block" />
        </div>
      ))}
    </div>
  );
}
