// app/(app)/tasks/page.tsx — OneVoice (Linen) Phase 4.7 rebuild
//
// What this page is:
//   The task queue feed for the business owner. Every row is something
//   OneVoice (or a platform agent) did on their behalf — publish a post,
//   reply to a comment, sync data, etc.
//
// What changed in Linen:
//   - PageHeader primitive instead of a stub h1
//   - Dot-coded mini-stats at the top (queued/active/done/error)
//   - Tasks list as a paper-raised card with paper-well expanded panels
//   - Expanded row shows two panels side-by-side:
//       1. Terminal-style log trace (paper-well, mono, line-prefixed)
//       2. "Подробности" KV card (paper-sunken) + plain-Russian
//          explanation (what + why + what to do next) + Reconnect CTA
//   - Brand voice: failures are NEVER raw stack traces in the human-
//     facing pane. The terminal panel can carry the raw error; the KV
//     panel restates it humanly.
//
// Mocks: design_handoff_onevoice 2/mocks/mock-tasks.jsx
// Brand voice: design_handoff_onevoice 2/Brand Voice Guide.md

'use client';

import { Fragment, useCallback, useMemo, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { format } from 'date-fns';
import { ru } from 'date-fns/locale';
import Link from 'next/link';
import { api } from '@/lib/api';
import { useTasksStream } from '@/hooks/useTasksStream';
import type { AgentTask, TaskStreamEvent } from '@/types/task';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { ChannelMark } from '@/components/ui/channel-mark';
import { MonoLabel } from '@/components/ui/mono-label';
import { PageHeader } from '@/components/ui/page-header';
import { Skeleton } from '@/components/ui/skeleton';
import { EmptyTasks } from '@/components/states';
import { cn } from '@/lib/utils';

// ─── Status / platform vocabulary ───────────────────────────────────
//
// The API speaks: pending | running | done | error. We translate to
// the brand-voice vocabulary: "В очереди / В работе / Готово / Нужна
// помощь" — note that we never call an error an "Ошибка" in the row
// label (only in the terminal log), per Brand Voice Guide §3.

type TaskStatus = 'pending' | 'running' | 'done' | 'error';

const statusLabel: Record<TaskStatus, string> = {
  pending: 'В очереди',
  running: 'В работе',
  done: 'Готово',
  error: 'Нужна помощь',
};

// Status → badge tone. `info` for queued reads quiet on warm paper.
const statusTone: Record<
  TaskStatus,
  'info' | 'accent' | 'success' | 'danger'
> = {
  pending: 'info',
  running: 'accent',
  done: 'success',
  error: 'danger',
};

// Dot color used in the mini-stats strip at top of page. Matches the
// badge tone for the same status — visual rhyme.
const statDotClass: Record<TaskStatus, string> = {
  pending: 'bg-info',
  running: 'bg-ochre',
  done: 'bg-success',
  error: 'bg-danger',
};

const platformLabel: Record<string, string> = {
  telegram: 'Telegram',
  vk: 'VK',
  yandex_business: 'Yandex.Business',
};

// Channel-mark display name (passed to ChannelMark which colors the
// disc). Yandex.Business already has a tuned brand colour in the
// primitive's table.
const platformChannelName: Record<string, string> = {
  telegram: 'Telegram',
  vk: 'VK',
  yandex_business: 'Yandex.Business',
};

// ─── Plain-Russian error explainer ──────────────────────────────────
//
// We never show a raw stack trace in the human-facing "Подробности"
// panel. We classify the error string and produce a what + why + what-
// to-do-next sentence per Brand Voice Guide §3 ("explain plainly, no
// exclamation marks").
//
// Order matters: the first regex hit wins. The fallback covers
// genuinely unknown errors with a calm, non-alarming line.

interface HumanError {
  /** Headline restating the failure in human terms. */
  summary: string;
  /** What the user can do, if anything actionable exists. */
  cta?: { label: string; href: string };
  /** True when the system will retry by itself — no user action needed. */
  willAutoRetry?: boolean;
}

function explainError(task: AgentTask): HumanError {
  const raw = (task.error ?? '').toLowerCase();
  const platform = task.platform;

  if (/token|unauthor|401|403|expired|истёк|истек/.test(raw)) {
    return {
      summary:
        platform === 'vk'
          ? 'Похоже, токен ВКонтакте истёк — возможно, владелец сообщества отозвал доступ. Переподключите канал, на это уйдёт минута.'
          : platform === 'telegram'
            ? 'Telegram больше не принимает наш токен — возможно, бот был исключён из канала. Нужно переподключить.'
            : 'Доступ к платформе истёк. Переподключите канал, чтобы мы могли продолжить.',
      cta: { label: 'Переподключить', href: '/integrations' },
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

  // Fallback — calm, non-alarming, no exclamation marks.
  return {
    summary:
      'Что-то пошло не так. Подробности — в журнале справа. Мы попробуем ещё раз при следующей синхронизации.',
    willAutoRetry: true,
  };
}

// ─── Terminal log synthesis ─────────────────────────────────────────
//
// The backend doesn't yet ship a structured log array on AgentTask.
// We synthesise a faithful trace from the lifecycle fields we DO have
// (createdAt → startedAt → completedAt) plus input/output JSON. When
// per-line logs land on the model later, replace this block with the
// real trace — the rest of the layout doesn't need to change.

interface LogLine {
  ts: string; // pre-formatted HH:mm:ss for the gutter
  text: string;
  level: 'info' | 'warn' | 'error';
}

function buildLogLines(task: AgentTask): LogLine[] {
  const lines: LogLine[] = [];
  const fmt = (iso: string | undefined) => {
    if (!iso) return '--:--:--';
    try {
      return format(new Date(iso), 'HH:mm:ss');
    } catch {
      return '--:--:--';
    }
  };

  lines.push({
    ts: fmt(task.createdAt),
    text: `task.created  type=${task.type} platform=${task.platform}`,
    level: 'info',
  });

  if (task.input != null) {
    lines.push({
      ts: fmt(task.createdAt),
      text: `task.input    ${JSON.stringify(task.input)}`,
      level: 'info',
    });
  }

  if (task.startedAt) {
    lines.push({
      ts: fmt(task.startedAt),
      text: 'task.started  dispatching to agent',
      level: 'info',
    });
  }

  if (task.output != null) {
    lines.push({
      ts: fmt(task.completedAt ?? task.startedAt),
      text: `task.output   ${JSON.stringify(task.output)}`,
      level: 'info',
    });
  }

  if (task.error) {
    lines.push({
      ts: fmt(task.completedAt ?? task.startedAt),
      text: `task.error    ${task.error}`,
      level: 'error',
    });
  }

  if (task.completedAt) {
    lines.push({
      ts: fmt(task.completedAt),
      text: task.error ? 'task.failed' : 'task.done',
      level: task.error ? 'error' : 'info',
    });
  } else if (task.status === 'running') {
    lines.push({ ts: '--:--:--', text: 'task.running …', level: 'info' });
  } else if (task.status === 'pending') {
    lines.push({
      ts: '--:--:--',
      text: 'task.queued   waiting for an agent slot',
      level: 'info',
    });
  }

  return lines;
}

// ─── Top-level page ─────────────────────────────────────────────────

export default function TasksPage() {
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const queryClient = useQueryClient();

  const { data: tasks = [], isLoading } = useQuery<AgentTask[]>({
    queryKey: ['tasks'],
    queryFn: () =>
      api.get('/tasks').then((r) => (r.data.tasks ?? []) as AgentTask[]),
    // SSE drives realtime; this is the safety net for dropped streams.
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
    const c: Record<TaskStatus, number> = {
      pending: 0,
      running: 0,
      done: 0,
      error: 0,
    };
    for (const t of tasks) {
      const s = (t.status as TaskStatus) ?? 'pending';
      if (s in c) c[s] += 1;
    }
    return c;
  }, [tasks]);

  return (
    <div className="min-h-screen bg-paper">
      <PageHeader
        title="Задачи"
        sub="Что OneVoice делает прямо сейчас и что не получилось"
      />

      {/* Dot-coded mini-stats. Compact, mono numbers, ink-mid labels. */}
      <div className="flex flex-wrap items-center gap-x-6 gap-y-3 px-12 pb-6">
        <StatDot status="pending" label="В очереди" value={counts.pending} />
        <StatDot status="running" label="В работе" value={counts.running} />
        <StatDot status="done" label="Готово" value={counts.done} />
        <StatDot status="error" label="Нужна помощь" value={counts.error} />
      </div>

      {/* Task list card */}
      <div className="px-12 pb-16">
        {isLoading ? (
          <TaskListSkeleton />
        ) : tasks.length === 0 ? (
          <EmptyState />
        ) : (
          <div className="overflow-hidden rounded-md border border-line bg-paper-raised shadow-ov-1">
            {tasks.map((task, idx) => (
              <TaskRow
                key={task.id}
                task={task}
                last={idx === tasks.length - 1}
                expanded={expandedId === task.id}
                onToggle={() =>
                  setExpandedId(expandedId === task.id ? null : task.id)
                }
              />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

// ─── Mini-stat (dot + count + label) ────────────────────────────────

function StatDot({
  status,
  label,
  value,
}: {
  status: TaskStatus;
  label: string;
  value: number;
}) {
  return (
    <div className="flex items-center gap-2">
      <span
        aria-hidden
        className={cn('size-2 rounded-full', statDotClass[status])}
      />
      <span className="font-mono text-[13px] font-medium tabular-nums text-ink">
        {value}
      </span>
      <span className="text-[13px] text-ink-mid">{label}</span>
    </div>
  );
}

// ─── Single row in the task list ────────────────────────────────────

interface TaskRowProps {
  task: AgentTask;
  last: boolean;
  expanded: boolean;
  onToggle: () => void;
}

function TaskRow({ task, last, expanded, onToggle }: TaskRowProps) {
  const status = (task.status as TaskStatus) ?? 'pending';
  const platformName = platformLabel[task.platform] ?? task.platform;
  const channelName = platformChannelName[task.platform] ?? platformName;

  const headerClass = cn(
    'grid w-full cursor-pointer grid-cols-[160px_1fr_auto_220px] items-center gap-4 px-5 py-4 text-left transition-colors hover:bg-paper-sunken',
    expanded && 'bg-paper-sunken'
  );

  return (
    <div className={cn(!last && 'border-b border-line-soft')}>
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={expanded}
        className={headerClass}
      >
        {/* Timestamp */}
        <span className="font-mono text-[12px] tabular-nums text-ink-soft">
          {format(new Date(task.createdAt), 'd MMM HH:mm', { locale: ru })}
        </span>

        {/* Tool name (mono) + display name fallback */}
        <span className="min-w-0 truncate font-mono text-[13px] text-ink">
          {task.displayName || task.type}
        </span>

        {/* Status badge with leading dot */}
        <Badge tone={statusTone[status]} dot>
          {statusLabel[status]}
        </Badge>

        {/* Target = ChannelMark + platform label */}
        <span className="flex items-center gap-2 justify-self-end">
          <ChannelMark name={channelName} size={20} />
          <span className="text-[13px] text-ink-mid">{platformName}</span>
        </span>
      </button>

      {expanded && <ExpandedPanel task={task} />}
    </div>
  );
}

// ─── Expanded panel: terminal log + KV "Подробности" ────────────────

const LOG_PREVIEW_LINES = 12;

function ExpandedPanel({ task }: { task: AgentTask }) {
  const lines = useMemo(() => buildLogLines(task), [task]);
  const [showAll, setShowAll] = useState(false);
  const visible = showAll ? lines : lines.slice(0, LOG_PREVIEW_LINES);
  const truncated = lines.length > LOG_PREVIEW_LINES && !showAll;

  const human = task.error ? explainError(task) : null;
  const platformName = platformLabel[task.platform] ?? task.platform;

  return (
    <div className="grid gap-4 border-t border-line-soft bg-paper-sunken/50 p-5 lg:grid-cols-[3fr_2fr]">
      {/* Terminal log trace */}
      <section className="flex flex-col rounded-md border border-line-soft bg-paper-well p-4">
        <MonoLabel tone="mid" className="mb-3">
          Журнал
        </MonoLabel>
        <pre className="m-0 max-h-72 overflow-auto whitespace-pre-wrap break-words font-mono text-[12px] leading-relaxed text-ink">
          {visible.map((l, i) => (
            <div
              key={i}
              className={cn(
                'flex gap-3',
                l.level === 'error' && 'text-[var(--ov-danger)]',
                l.level === 'warn' && 'text-[var(--ov-warning-ink)]'
              )}
            >
              <span className="select-none text-ink-faint">{l.ts}</span>
              <span className="flex-1">{l.text}</span>
            </div>
          ))}
        </pre>
        {truncated && (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="mt-3 self-start"
            onClick={() => setShowAll(true)}
          >
            Показать всё ({lines.length})
          </Button>
        )}
      </section>

      {/* KV "Подробности" */}
      <aside className="flex flex-col gap-3 rounded-md border border-line-soft bg-paper-sunken p-4">
        <MonoLabel tone="mid">Подробности</MonoLabel>

        <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-[13px]">
          <KV k="Платформа" v={platformName} />
          <KV
            k="Канал"
            v={
              extractChannelId(task) ?? (
                <span className="text-ink-soft">не указан</span>
              )
            }
          />
          {task.startedAt && (
            <KV
              k="Начало"
              v={format(new Date(task.startedAt), 'd MMM HH:mm:ss', {
                locale: ru,
              })}
            />
          )}
          {task.completedAt && (
            <KV
              k="Завершено"
              v={format(new Date(task.completedAt), 'd MMM HH:mm:ss', {
                locale: ru,
              })}
            />
          )}
          {task.error && <KV k="Ошибка" v={task.error} mono />}
        </dl>

        {human && (
          <div className="mt-1 flex flex-col gap-3 rounded-md border border-line-soft bg-paper-raised p-3">
            <p className="text-[13px] leading-relaxed text-ink">
              {human.summary}
            </p>
            {human.cta && (
              <Link href={human.cta.href} className="self-start">
                <Button variant="primary" size="sm">
                  {human.cta.label}
                </Button>
              </Link>
            )}
            {human.willAutoRetry && (
              <p className="text-[12px] text-ink-soft">
                Можно ничего не нажимать — мы повторим сами.
              </p>
            )}
          </div>
        )}
      </aside>
    </div>
  );
}

// Render a key-value row inside the dl. Values that are strings get
// truncated with a tooltip (title attr) so long error messages don't
// blow out the panel width.
function KV({
  k,
  v,
  mono = false,
}: {
  k: string;
  v: React.ReactNode;
  mono?: boolean;
}) {
  return (
    <>
      <dt className="text-ink-soft">{k}</dt>
      <dd
        className={cn(
          'min-w-0 break-words text-ink',
          mono && 'font-mono text-[12px]'
        )}
        title={typeof v === 'string' ? v : undefined}
      >
        {v}
      </dd>
    </>
  );
}

// Best-effort channel-id extractor from task.input. The orchestrator
// passes various shapes (channel_id, chat_id, group_id, external_id);
// we surface the first one present so the KV row reads usefully even
// before the agent runs.
function extractChannelId(task: AgentTask): string | null {
  const input = task.input as Record<string, unknown> | null | undefined;
  if (!input || typeof input !== 'object') return null;
  for (const key of ['channel_id', 'chat_id', 'group_id', 'external_id']) {
    const v = input[key];
    if (typeof v === 'string' && v.length > 0) return v;
    if (typeof v === 'number') return String(v);
  }
  return null;
}

// ─── Empty / loading states ─────────────────────────────────────────

function EmptyState() {
  // Linen empty-state per mock-states.jsx "Все задачи закрыты": single
  // factual sentence, no celebration. EmptyTasks owns the copy + frame.
  return <EmptyTasks />;
}

function TaskListSkeleton() {
  return (
    <div className="overflow-hidden rounded-md border border-line bg-paper-raised shadow-ov-1">
      {Array.from({ length: 6 }, (_, i) => (
        <div
          key={i}
          className={cn(
            'grid grid-cols-[160px_1fr_auto_220px] items-center gap-4 px-5 py-4',
            i < 5 && 'border-b border-line-soft'
          )}
        >
          <Skeleton className="h-4 w-24" />
          <Skeleton className="h-4 w-64" />
          <Skeleton className="h-5 w-24 rounded-full" />
          <Skeleton className="h-5 w-32 justify-self-end" />
        </div>
      ))}
    </div>
  );
}
