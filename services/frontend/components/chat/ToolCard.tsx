import { Pencil } from 'lucide-react';

import type { ToolCall } from '@/types/chat';
import { PLATFORM_COLORS, PLATFORM_LABELS, getPlatform } from '@/lib/platforms';
import { cn } from '@/lib/utils';
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
      : 'text-gray-600'
  );

  return (
    <div
      className="space-y-1 rounded-md border p-3 text-sm"
      style={{ borderLeftColor, borderLeftWidth: 3 }}
    >
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <span
            className="rounded px-1.5 py-0.5 text-xs font-bold text-white"
            style={{ backgroundColor: color }}
          >
            {label}
          </span>
          <span className={toolNameClasses}>{tool.name}</span>
        </div>
        {tool.status === 'pending' && (
          <span className="h-4 w-4 animate-spin rounded-full border-2 border-gray-300 border-t-blue-500" />
        )}
        {tool.status === 'done' && <span className="text-green-500">✅</span>}
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
        {tool.status === 'error' && <span className="text-red-500">❌</span>}
        {tool.status === 'aborted' && (
          <span className="text-gray-500" title="Выполнение прервано — результат не получен">
            ⏸
          </span>
        )}
        {tool.status === 'rejected' && (
          <span className="inline-flex items-center gap-1 rounded-md bg-destructive/10 px-2 py-0.5 text-xs font-semibold text-destructive">
            {RU.rejectedBadge}
          </span>
        )}
        {tool.status === 'expired' && (
          <span className="inline-flex items-center gap-1 rounded-md border border-amber-300 bg-amber-100 px-2 py-0.5 text-xs font-semibold text-amber-900">
            {RU.expiredBadge}
          </span>
        )}
      </div>
      {tool.result && (
        <p className="truncate text-xs text-gray-500">{JSON.stringify(tool.result).slice(0, 80)}</p>
      )}
      {tool.error && <p className="text-xs text-red-500">{tool.error}</p>}
      {tool.status === 'rejected' && tool.rejectReason && (
        <p className="text-xs italic text-muted-foreground">
          {RU.rejectedReasonPrefix}
          {tool.rejectReason}
        </p>
      )}
      {tool.status === 'aborted' && (
        <p className="text-xs italic text-gray-500">Выполнение прервано — результат не получен</p>
      )}
    </div>
  );
}
