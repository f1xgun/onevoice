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
//     auto-retry assurance.
//
// No expand/collapse, no terminal log, no KV "Подробности". Brand Voice
// Guide §3: failures explain what + why + what-to-do-next, calmly.

'use client';

import { useCallback, useMemo } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { format } from 'date-fns';
import { ru } from 'date-fns/locale';
import Link from 'next/link';
import { api } from '@/lib/api';
import { useTasksStream } from '@/hooks/useTasksStream';
import type { AgentTask, TaskStreamEvent } from '@/types/task';
import { Button } from '@/components/ui/button';
import { ChannelMark } from '@/components/ui/channel-mark';
import { MonoLabel } from '@/components/ui/mono-label';
import { PageHeader } from '@/components/ui/page-header';
import { Skeleton } from '@/components/ui/skeleton';
import { EmptyTasks } from '@/components/states';
import { cn } from '@/lib/utils';

// ─── Status / platform vocabulary ───────────────────────────────────

type TaskStatus = 'pending' | 'running' | 'done' | 'error';

const statusLabel: Record<TaskStatus, string> = {
  pending: 'Запланировано',
  running: 'В работе',
  done: 'Готово',
  error: 'Нужна помощь',
};

const statusDotClass: Record<TaskStatus, string> = {
  pending: 'bg-ink-faint',
  running: 'bg-ochre',
  done: 'bg-success',
  error: 'bg-danger',
};

const platformLabel: Record<string, string> = {
  telegram: 'Telegram',
  vk: 'VK',
  yandex_business: 'Yandex.Business',
};

// ─── Plain-Russian error explainer ──────────────────────────────────
//
// Brand Voice Guide §3: explain what + why + what-to-do-next, calmly,
// no exclamation marks. We never surface the raw error string.

interface HumanError {
  summary: string;
  cta?: { label: string; href: string };
  willAutoRetry?: boolean;
}

function explainError(task: AgentTask): HumanError {
  const raw = (task.error ?? '').toLowerCase();
  const platform = task.platform;

  if (/token|unauthor|401|403|expired|истёк|истек/.test(raw)) {
    return {
      summary:
        platform === 'vk'
          ? 'Похоже, доступ к сообществу ВКонтакте истёк. Это бывает раз в пару недель — нужно переподключить канал, на это уйдёт минута.'
          : platform === 'telegram'
            ? 'Telegram больше не принимает наш токен — возможно, бот был исключён из канала. Нужно переподключить.'
            : 'Доступ к платформе истёк. Переподключите канал, чтобы мы могли продолжить.',
      cta: {
        label: platform === 'vk' ? 'Переподключить ВКонтакте' : 'Переподключить',
        href: '/integrations',
      },
    };
  }

  if (/rate.?limit|too many|429/.test(raw)) {
    return {
      summary:
        'Платформа попросила подождать — мы попробуем ещё раз через несколько минут автоматически.',
      willAutoRetry: true,
    };
  }

  if (/timeout|deadline|временно|unavailable|503|502|504/.test(raw)) {
    return {
      summary:
        'Сервис временно не отвечал. Уже попробовали несколько раз — повторим при следующей синхронизации.',
      willAutoRetry: true,
    };
  }

  if (/not.?found|404|канал.*не/.test(raw)) {
    return {
      summary:
        'Канал не найден. Проверьте, что он всё ещё подключён и доступен.',
      cta: { label: 'Открыть каналы', href: '/integrations' },
    };
  }

  if (/photo|image|media|too.?large|размер/.test(raw)) {
    return {
      summary:
        'Не получилось загрузить изображение — возможно, файл слишком большой или платформа его отклонила.',
    };
  }

  return {
    summary:
      'Что-то пошло не так. Мы попробуем ещё раз при следующей синхронизации.',
    willAutoRetry: true,
  };
}

// ─── Top-level page ─────────────────────────────────────────────────

export default function TasksPage() {
  const queryClient = useQueryClient();

  const { data: tasks = [], isLoading } = useQuery<AgentTask[]>({
    queryKey: ['tasks'],
    queryFn: () =>
      api.get('/tasks').then((r) => {
        const data = r.data as unknown;
        if (Array.isArray(data)) return data as AgentTask[];
        const list = (data as { tasks?: AgentTask[] } | null)?.tasks;
        return Array.isArray(list) ? list : [];
      }),
    refetchInterval: 30_000,
  });

  const onStreamEvent = useCallback(
    (_: TaskStreamEvent) => {
      queryClient.invalidateQueries({ queryKey: ['tasks'] });
    },
    [queryClient]
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

  const doneToday = counts.done;
  const inFlight = counts.running;
  const needsHelp = counts.error;

  return (
    <div className="min-h-screen bg-paper">
      <PageHeader
        title="Что сделано"
        sub="Здесь видно всё, что OneVoice делал от вашего имени. Можно ничего не нажимать — мы сами повторим, если что-то не получилось."
      />

      {/* BigStat tiles per v2 mock */}
      <div className="grid grid-cols-1 gap-3 px-4 pb-6 sm:px-12 sm:grid-cols-3">
        <BigStat
          label="Сделано"
          value={doneToday}
          hint={
            doneToday === 0
              ? 'пока ничего'
              : `из них ${doneToday} успешно, ${needsHelp} ${needsHelpPlural(needsHelp)} вашего внимания`
          }
          tone="default"
        />
        <BigStat
          label="Сейчас в работе"
          value={inFlight}
          hint={inFlight === 0 ? 'нет активных задач' : 'идёт прямо сейчас'}
          tone={inFlight > 0 ? 'accent' : 'default'}
        />
        <BigStat
          label="Ждут вашего шага"
          value={needsHelp}
          hint={needsHelp === 0 ? 'всё в порядке' : 'требуется ваше внимание'}
          tone={needsHelp > 0 ? 'warning' : 'default'}
        />
      </div>

      {/* Task list */}
      <div className="px-4 pb-16 sm:px-12">
        {isLoading ? (
          <TaskListSkeleton />
        ) : tasks.length === 0 ? (
          <EmptyTasks />
        ) : (
          <div className="overflow-hidden rounded-md border border-line bg-paper-raised shadow-ov-1">
            {tasks.map((task, idx) => (
              <TaskRow key={task.id} task={task} last={idx === tasks.length - 1} />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

// ─── Single row (no expand/collapse) ────────────────────────────────

function TaskRow({ task, last }: { task: AgentTask; last: boolean }) {
  const status = (task.status as TaskStatus) ?? 'pending';
  const platformName = platformLabel[task.platform] ?? task.platform;
  const titleClass =
    status === 'error'
      ? 'text-sm font-medium text-[var(--ov-danger-ink)] tracking-[-0.005em]'
      : 'text-sm font-medium text-ink tracking-[-0.005em]';
  const human = status === 'error' ? explainError(task) : null;

  return (
    <div className={cn(!last && 'border-b border-line-soft')}>
      <div className="flex items-start gap-3 px-4 py-3 sm:grid sm:grid-cols-[24px_1fr_160px_180px] sm:items-center sm:gap-4 sm:px-5 sm:py-4">
        {/* Status dot — vertically centered on the title line for mobile. */}
        <span
          aria-hidden
          className={cn('mt-1.5 size-2 shrink-0 rounded-full sm:mt-0 sm:justify-self-center', statusDotClass[status])}
        />

        {/* Title (+ optional one-line detail).
            On mobile, platform + when fold underneath the title as a single
            meta line so the row never exceeds the viewport width. */}
        <div className="min-w-0 flex-1">
          <div className={titleClass}>{task.displayName || task.type}</div>
          {status === 'done' && typeof task.output === 'string' && task.output.trim().length > 0 && (
            <div className="mt-0.5 truncate text-[13px] text-ink-mid">{task.output}</div>
          )}
          <div className="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-[12px] text-ink-soft sm:hidden">
            <span className="inline-flex items-center gap-1.5">
              <ChannelMark name={platformName} size={16} />
              {platformName}
            </span>
            <span aria-hidden>·</span>
            <span>{format(new Date(task.createdAt), 'd MMM HH:mm', { locale: ru })}</span>
            <span aria-hidden>·</span>
            <span>{statusLabel[status]}</span>
          </div>
        </div>

        {/* Desktop-only: platform column */}
        <span className="hidden items-center gap-2 sm:flex">
          <ChannelMark name={platformName} size={20} />
          <span className="text-[13px] text-ink-mid">{platformName}</span>
        </span>

        {/* Desktop-only: when · status label column */}
        <div className="hidden items-center justify-between gap-3 sm:flex">
          <span className="text-[13px] text-ink-mid">
            {format(new Date(task.createdAt), 'd MMM HH:mm', { locale: ru })}
          </span>
          <span className="text-xs text-ink-soft">{statusLabel[status]}</span>
        </div>
      </div>

      {/* Inline human-only warning callout — no log, no JSON, no IDs. */}
      {human && (
        <div className="mx-4 mb-4 flex items-start gap-3 rounded-md border border-[oklch(0.85_0.10_75)] bg-warning-soft px-4 py-3 sm:mx-5 sm:ml-[52px]">
          <span
            aria-hidden
            className="mt-0.5 inline-flex size-5 shrink-0 items-center justify-center rounded-full bg-warning text-[12px] font-semibold text-paper"
          >
            !
          </span>
          <div className="flex-1">
            <p className="text-[14px] leading-relaxed text-warning-ink">{human.summary}</p>
            {human.cta && (
              <div className="mt-3">
                <Link href={human.cta.href}>
                  <Button variant="primary" size="sm">
                    {human.cta.label}
                  </Button>
                </Link>
              </div>
            )}
            {human.willAutoRetry && (
              <p className="mt-2 text-xs text-ink-soft">
                Можно ничего не нажимать — мы попробуем ещё раз сами.
              </p>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

// ─── BigStat tile ───────────────────────────────────────────────────

function BigStat({
  label,
  value,
  hint,
  tone,
}: {
  label: string;
  value: number | string;
  hint: string;
  tone: 'default' | 'accent' | 'warning';
}) {
  const toneClass = {
    default: 'border-line bg-paper-raised',
    accent: 'border-[oklch(0.85_0.06_75)] bg-accent-soft',
    warning: 'border-[oklch(0.85_0.10_75)] bg-warning-soft',
  }[tone];
  return (
    <div className={cn('flex flex-col gap-2 rounded-md border px-4 py-4 sm:px-6 sm:py-5', toneClass)}>
      <MonoLabel tone={tone === 'accent' ? 'ochre' : tone === 'warning' ? 'mid' : 'soft'}>
        {label}
      </MonoLabel>
      <span className="font-mono text-[32px] font-medium leading-none tracking-[-0.02em] tabular-nums text-ink">
        {value}
      </span>
      <span className="text-[13px] leading-relaxed text-ink-mid">{hint}</span>
    </div>
  );
}

function needsHelpPlural(n: number): string {
  const last = n % 10;
  const lastTwo = n % 100;
  if (lastTwo >= 11 && lastTwo <= 14) return 'требуют';
  if (last === 1) return 'требует';
  if (last >= 2 && last <= 4) return 'требуют';
  return 'требуют';
}

// ─── Loading skeleton ───────────────────────────────────────────────

function TaskListSkeleton() {
  return (
    <div className="overflow-hidden rounded-md border border-line bg-paper-raised shadow-ov-1">
      {Array.from({ length: 6 }, (_, i) => (
        <div
          key={i}
          className={cn(
            'flex items-center gap-3 px-4 py-3 sm:grid sm:grid-cols-[24px_1fr_160px_180px] sm:gap-4 sm:px-5 sm:py-4',
            i < 5 && 'border-b border-line-soft'
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
