// components/chat/ToolCard.tsx — OneVoice (Linen) tool-call card
//
// One tool call = one card with the platform tag, the tool name in
// JetBrains Mono, and a status pill on the right. The card carries a
// 3 px platform-tinted left border (hsl(var(--destructive)) when the
// user rejected the call). Linen background + 1 px line border on the
// rest of the card.
//
// Phase 17 visual contracts preserved verbatim (rejected/expired badges,
// line-through name, Pencil tooltip, "Причина:" copy). Test fixtures in
// components/chat/__tests__/ToolCard.{rejected,expired,edited}.test.tsx
// pin these classes/strings — this rebuild keeps them.

import { Pencil } from 'lucide-react';

import type { ToolCall } from '@/types/chat';
import { PLATFORM_COLORS, PLATFORM_LABELS, getPlatform } from '@/lib/platforms';
import { cn } from '@/lib/utils';
import { Badge } from '@/components/ui/badge';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip';

// Exact Russian literals from 17-UI-SPEC §Copywriting Contract (Post-submit
// in-history visuals) + §Expired approval banner / Rejected tool visual.
// Kept inline per 17-RESEARCH §Don't Hand-Roll (no shared i18n layer in v1.3).
const RU = {
  rejectedBadge: 'Отклонено пользователем',
  rejectedReasonPrefix: 'Причина: ',
  expiredBadge: 'Истекло',
  editedTooltip: 'Аргументы изменены пользователем',
} as const;

export function ToolCard({ tool }: { tool: ToolCall }) {
  const platform = getPlatform(tool.name);
  const color = PLATFORM_COLORS[platform] ?? '#6b7280';
  const label = PLATFORM_LABELS[platform] ?? platform.toUpperCase();

  // Rejection takes visual priority over the platform accent (17-UI-SPEC
  // §Post-submit Rejected tool). Expired keeps the platform color — the
  // banner above the history carries the primary "expired" signal.
  const borderLeftColor = tool.status === 'rejected' ? 'hsl(var(--destructive))' : color;

  // Struck-through name for both rejected and expired terminal states.
  const toolNameClasses = cn(
    'font-mono text-xs',
    tool.status === 'rejected' || tool.status === 'expired'
      ? 'line-through text-muted-foreground'
      : 'text-ink-mid'
  );

  return (
    <div
      className="space-y-1 rounded-md border border-line bg-paper-raised p-3 text-sm"
      style={{ borderLeftColor, borderLeftWidth: 3 }}
    >
      <div className="flex items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2">
          <span
            className="rounded px-1.5 py-0.5 text-xs font-bold text-paper"
            style={{ backgroundColor: color }}
          >
            {label}
          </span>
          <span className={toolNameClasses}>{tool.name}</span>
        </div>
        {tool.status === 'pending' && (
          <Badge tone="info" dot aria-label="Выполняется">
            <span className="h-3 w-3 animate-spin rounded-full border-2 border-line border-t-blue-500" />
            Выполняется
          </Badge>
        )}
        {tool.status === 'done' && (
          <Badge tone="success" aria-label="Готово">
            <span className="text-[var(--ov-success)]">✅</span>
            Готово
          </Badge>
        )}
        {tool.status === 'done' && tool.wasEdited && (
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger asChild>
                <span className="inline-flex cursor-help items-center">
                  <Pencil
                    size={12}
                    className="text-muted-foreground"
                    aria-label={RU.editedTooltip}
                  />
                </span>
              </TooltipTrigger>
              <TooltipContent>{RU.editedTooltip}</TooltipContent>
            </Tooltip>
          </TooltipProvider>
        )}
        {tool.status === 'error' && (
          <Badge tone="danger" aria-label="Ошибка">
            <span className="text-[var(--ov-danger)]">❌</span>
            Ошибка
          </Badge>
        )}
        {tool.status === 'aborted' && (
          <Badge
            tone="neutral"
            aria-label="Прервано"
            title="Выполнение прервано — результат не получен"
          >
            <span className="text-ink-soft">⏸</span>
            Прервано
          </Badge>
        )}
        {tool.status === 'rejected' && (
          <Badge tone="danger" className="text-destructive">
            {RU.rejectedBadge}
          </Badge>
        )}
        {tool.status === 'expired' && <Badge tone="warning">{RU.expiredBadge}</Badge>}
      </div>
      {tool.result && summarizeResult(tool.name, tool.result) && (
        <p className="text-xs text-ink-soft">{summarizeResult(tool.name, tool.result)}</p>
      )}
      {tool.error && <p className="text-xs text-[var(--ov-danger)]">{tool.error}</p>}
      {tool.status === 'rejected' && tool.rejectReason && (
        <p className="text-xs italic text-muted-foreground">
          {RU.rejectedReasonPrefix}
          {tool.rejectReason}
        </p>
      )}
      {tool.status === 'aborted' && (
        <p className="text-xs italic text-ink-soft">Выполнение прервано — результат не получен</p>
      )}
    </div>
  );
}

// Human-readable, locale-aware summary of a tool result. Returns null when
// nothing useful can be said — in that case the success badge alone is the
// signal. Per brand voice we never surface raw JSON to the operator.
function summarizeResult(toolName: string, result: unknown): string | null {
  if (!result || typeof result !== 'object') return null;
  const r = result as Record<string, unknown>;

  // Common get-list shapes across platforms.
  const lists: { key: string; word: (n: number) => string }[] = [
    { key: 'reviews', word: pluralReviews },
    { key: 'comments', word: pluralComments },
    { key: 'posts', word: pluralPosts },
    { key: 'messages', word: pluralMessages },
    { key: 'items', word: pluralItems },
  ];
  for (const { key, word } of lists) {
    const v = r[key];
    if (Array.isArray(v)) {
      const n = typeof r.count === 'number' ? r.count : v.length;
      return n === 0 ? `Ничего не нашлось` : `Получено: ${n} ${word(n)}`;
    }
  }
  if (typeof r.count === 'number') {
    return r.count === 0 ? 'Ничего не нашлось' : `Получено: ${r.count}`;
  }

  // Send-action shape — orchestrator returns ok/sent/posted booleans.
  if (toolName.includes('send') || toolName.includes('post') || toolName.includes('reply')) {
    if (r.ok === true || r.sent === true || r.posted === true) return 'Отправлено';
  }

  return null;
}

function pluralRu(n: number, [one, few, many]: [string, string, string]): string {
  const last = n % 10;
  const lastTwo = n % 100;
  if (lastTwo >= 11 && lastTwo <= 14) return many;
  if (last === 1) return one;
  if (last >= 2 && last <= 4) return few;
  return many;
}
const pluralReviews = (n: number) => pluralRu(n, ['отзыв', 'отзыва', 'отзывов']);
const pluralComments = (n: number) => pluralRu(n, ['комментарий', 'комментария', 'комментариев']);
const pluralPosts = (n: number) => pluralRu(n, ['пост', 'поста', 'постов']);
const pluralMessages = (n: number) => pluralRu(n, ['сообщение', 'сообщения', 'сообщений']);
const pluralItems = (n: number) => pluralRu(n, ['элемент', 'элемента', 'элементов']);
