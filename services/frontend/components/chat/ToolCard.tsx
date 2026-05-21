'use client';

// components/chat/ToolCard.tsx — OneVoice (Linen) tool-call card
//
// One tool call = one card with the platform tag, the tool name in
// JetBrains Mono, and a status pill on the right. The card carries a
// 3 px platform-tinted left border (hsl(var(--destructive)) when the
// user rejected the call). Linen background + 1 px line border on the
// rest of the card.
//
// Visual contracts preserved verbatim (rejected/expired badges,
// line-through name, Pencil tooltip, "Причина:" copy). Test fixtures in
// components/chat/__tests__/ToolCard.{rejected,expired,edited}.test.tsx
// pin these classes/strings — this rebuild keeps them.

import { Pencil } from 'lucide-react';
import { useTranslations } from 'next-intl';

import type { ToolCall } from '@/types/chat';
import { PLATFORM_COLORS, PLATFORM_LABELS, getPlatform } from '@/lib/platforms';
import { cn } from '@/lib/utils';
import { Badge } from '@/components/ui/badge';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip';

export function ToolCard({ tool }: { tool: ToolCall }) {
  const tCard = useTranslations('chat.toolCard');
  // Phase D3 follow-up: render the locale-aware tool label when the
  // orchestrator stamped `displayNameKey` on the SSE frame. Mirrors the
  // `app/(app)/tasks/page.tsx` pattern — keys live under
  // `agentTasks.displayName.*` (e.g. `tools.telegram.send_channel_post.name`).
  // next-intl returns the namespaced key verbatim when a key is missing, so
  // a strict `resolved !== displayNameKey` comparison gives us the safe
  // fallback to the raw `tool.name` without a `t.has()` round-trip.
  const tToolNames = useTranslations('agentTasks.displayName');
  const platform = getPlatform(tool.name);
  const color = PLATFORM_COLORS[platform] ?? '#6b7280';
  const label = PLATFORM_LABELS[platform] ?? platform.toUpperCase();

  // Rejection takes visual priority over the platform accent (UI-SPEC
  // §Post-submit Rejected tool). Expired keeps the platform color — the
  // banner above the history carries the primary "expired" signal.
  const borderLeftColor = tool.status === 'rejected' ? 'hsl(var(--destructive))' : color;

  // Localized name resolution: prefer `t(displayNameKey)` when the key
  // both exists and resolves to something distinct from the input key
  // (next-intl's missing-key behavior). Falls back to the raw tool name
  // (the existing contract) for older orchestrator deploys, unknown
  // keys, or tools the catalog has not been updated for yet.
  const displayName = (() => {
    if (!tool.displayNameKey) return tool.name;
    const resolved = tToolNames(tool.displayNameKey);
    return resolved && resolved !== tool.displayNameKey ? resolved : tool.name;
  })();

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
          <span className={toolNameClasses}>{displayName}</span>
        </div>
        {tool.status === 'pending' && (
          <Badge tone="info" dot aria-label={tCard('running')}>
            <span className="h-3 w-3 animate-spin rounded-full border-2 border-line border-t-blue-500" />
            {tCard('running')}
          </Badge>
        )}
        {tool.status === 'done' && (
          <Badge tone="success" aria-label={tCard('done')}>
            <span className="text-[var(--ov-success)]">✅</span>
            {tCard('done')}
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
                    aria-label={tCard('editedTooltip')}
                  />
                </span>
              </TooltipTrigger>
              <TooltipContent>{tCard('editedTooltip')}</TooltipContent>
            </Tooltip>
          </TooltipProvider>
        )}
        {tool.status === 'error' && (
          <Badge tone="danger" aria-label={tCard('error')}>
            <span className="text-[var(--ov-danger)]">❌</span>
            {tCard('error')}
          </Badge>
        )}
        {tool.status === 'aborted' && (
          <Badge tone="neutral" aria-label={tCard('aborted')} title={tCard('abortedTooltip')}>
            <span className="text-ink-soft">⏸</span>
            {tCard('aborted')}
          </Badge>
        )}
        {tool.status === 'rejected' && (
          <Badge tone="danger" className="text-destructive">
            {tCard('rejectedBadge')}
          </Badge>
        )}
        {tool.status === 'expired' && <Badge tone="warning">{tCard('expiredBadge')}</Badge>}
      </div>
      {tool.result && summarizeResult(tCard, tool.name, tool.result) && (
        <p className="text-xs text-ink-soft">{summarizeResult(tCard, tool.name, tool.result)}</p>
      )}
      {tool.error && <p className="text-xs text-[var(--ov-danger)]">{tool.error}</p>}
      {tool.status === 'rejected' && tool.rejectReason && (
        <p className="text-xs italic text-muted-foreground">
          {tCard('rejectedReason', { reason: tool.rejectReason })}
        </p>
      )}
      {tool.status === 'aborted' && (
        <p className="text-xs italic text-ink-soft">{tCard('abortedNote')}</p>
      )}
    </div>
  );
}

// Human-readable, locale-aware summary of a tool result. Returns null when
// nothing useful can be said — in that case the success badge alone is the
// signal. Per brand voice we never surface raw JSON to the operator.
// The `t` translator is the request-scoped instance from `chat.toolCard`,
// passed in by the React component so the helper stays pure and the i18n
// strings (plurals + countless variants) resolve via ICU/next-intl.
type ToolCardTranslator = (key: string, values?: Record<string, string | number | Date>) => string;

function summarizeResult(t: ToolCardTranslator, toolName: string, result: unknown): string | null {
  if (!result || typeof result !== 'object') return null;
  const r = result as Record<string, unknown>;

  // Common get-list shapes across platforms. ICU plural keys live under
  // chat.toolCard.got* — one per noun (reviews / comments / posts / messages /
  // items). Each key uses RU one/few/many/other and EN one/other categories.
  const lists: { key: string; tKey: string }[] = [
    { key: 'reviews', tKey: 'gotReviews' },
    { key: 'comments', tKey: 'gotComments' },
    { key: 'posts', tKey: 'gotPosts' },
    { key: 'messages', tKey: 'gotMessages' },
    { key: 'items', tKey: 'gotItems' },
  ];
  for (const { key, tKey } of lists) {
    const v = r[key];
    if (Array.isArray(v)) {
      const n = typeof r.count === 'number' ? r.count : v.length;
      return n === 0 ? t('nothingFound') : t(tKey, { count: n });
    }
  }
  if (typeof r.count === 'number') {
    return r.count === 0 ? t('nothingFound') : t('gotCount', { count: r.count });
  }

  // Send-action shape — orchestrator returns ok/sent/posted booleans.
  if (toolName.includes('send') || toolName.includes('post') || toolName.includes('reply')) {
    if (r.ok === true || r.sent === true || r.posted === true) return t('sent');
  }

  return null;
}
