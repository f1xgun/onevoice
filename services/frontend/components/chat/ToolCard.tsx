'use client';

import { Pencil, Check, CircleAlert, Loader2, CircleHelp } from 'lucide-react';
import { useTranslations } from 'next-intl';

import type { ErrorCode, ToolCall } from '@/types/chat';
import { PLATFORM_LABELS, getPlatform } from '@/lib/platforms';
import { cn } from '@/lib/utils';
import { Badge } from '@/components/ui/badge';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip';

// System/policy rejections carry one of these rejectReason codes (emitted by
// the orchestrator). They get a distinct "blocked by policy" badge + a readable
// explanation instead of the user-rejection copy — a policy_forbidden was being
// mislabeled as "rejected by the user". Any other rejectReason is a user HITL
// rejection (free-text or empty).
const POLICY_REJECT_REASON_KEYS: Record<string, string> = {
  policy_forbidden: 'policyForbiddenReason',
  policy_revoked: 'policyRevokedReason',
};

// Typed tool-error classifiers (pkg/a2a.CodedError → tool_result.code) → their
// i18n key under chat.toolCard.errorSummary. The headline is always localized;
// the raw `tool.error` string is relegated to the expandable details. Unknown
// or missing codes resolve to the generic `errorFallback` line.
const TOOL_ERROR_SUMMARY_KEYS: Record<ErrorCode, string> = {
  integration_token_invalid: 'errorTokenInvalid',
  rate_limit_exceeded: 'errorRateLimit',
  transient: 'errorTransient',
  channel_not_found: 'errorNotFound',
  media_too_large: 'errorMedia',
};

function toolErrorSummaryKey(code: ErrorCode | undefined): string {
  return (code && TOOL_ERROR_SUMMARY_KEYS[code]) || 'errorFallback';
}

export function ToolCard({ tool }: { tool: ToolCall }) {
  const tCard = useTranslations('chat.toolCard');
  const tToolNames = useTranslations('agentTasks.displayName');
  const platform = getPlatform(tool.name);
  const label = PLATFORM_LABELS[platform] ?? platform.toUpperCase();

  const decisionUnavailable = tool.rejectReason === 'decision_unavailable';

  const policyReasonKey = tool.rejectReason
    ? POLICY_REJECT_REASON_KEYS[tool.rejectReason]
    : undefined;

  const displayName = (() => {
    if (!tool.displayNameKey) return tool.name;
    const resolved = tToolNames(tool.displayNameKey);
    return resolved && resolved !== tool.displayNameKey ? resolved : tool.name;
  })();

  const toolNameClasses = cn(
    'min-w-0 break-words text-meta',
    tool.status === 'rejected' || tool.status === 'expired'
      ? 'line-through text-muted-foreground'
      : 'text-ink-mid'
  );

  return (
    <div
      className={cn(
        'min-w-0 space-y-2 rounded-lg border border-line bg-paper-raised p-4 text-meta',
        tool.status === 'rejected' && !decisionUnavailable && 'border-l-danger'
      )}
    >
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2">
          <span className="rounded bg-paper-sunken px-2 py-1 text-meta text-ink">{label}</span>
          <span className={toolNameClasses}>{displayName}</span>
        </div>
        {tool.status === 'pending' && (
          <Badge tone="info" dot aria-label={tCard('running')}>
            <Loader2 aria-hidden className="h-4 w-4" />
            {tCard('running')}
          </Badge>
        )}
        {tool.status === 'done' && (
          <Badge tone="success" aria-label={tCard('done')}>
            <Check aria-hidden className="h-4 w-4" />
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
              <TooltipContent className="max-w-[calc(100vw-2rem)] border-control bg-card text-ink shadow-overlay">
                {tCard('editedTooltip')}
              </TooltipContent>
            </Tooltip>
          </TooltipProvider>
        )}
        {tool.status === 'error' && (
          <Badge tone="danger" aria-label={tCard('error')}>
            <CircleAlert aria-hidden className="h-4 w-4" />
            {tCard('error')}
          </Badge>
        )}
        {tool.status === 'aborted' && (
          <Badge tone="neutral" aria-label={tCard('aborted')} title={tCard('abortedTooltip')}>
            <CircleHelp aria-hidden className="h-4 w-4" />
            {tCard('aborted')}
          </Badge>
        )}
        {tool.status === 'rejected' && (
          <Badge
            tone={decisionUnavailable ? 'neutral' : 'danger'}
            className={decisionUnavailable ? undefined : 'text-destructive'}
          >
            {decisionUnavailable
              ? tCard('decisionUnavailableBadge')
              : policyReasonKey
                ? tCard('rejectedByPolicyBadge')
                : tCard('rejectedBadge')}
          </Badge>
        )}
        {tool.status === 'expired' && <Badge tone="warning">{tCard('expiredBadge')}</Badge>}
      </div>
      {tool.status === 'done' && tool.result && summarizeResult(tCard, tool.name, tool.result) && (
        <p className="text-xs text-ink-soft">{summarizeResult(tCard, tool.name, tool.result)}</p>
      )}
      {tool.error && (
        <div className="text-xs text-[var(--ov-danger)]">
          <p>{tCard(toolErrorSummaryKey(tool.code))}</p>
          <details className="mt-0.5">
            <summary className="cursor-pointer text-ink-soft">{tCard('errorDetailsLabel')}</summary>
            <p className="mt-0.5 whitespace-pre-wrap break-words font-mono text-ink-soft">
              {tool.error}
            </p>
          </details>
        </div>
      )}
      {tool.status === 'rejected' && tool.rejectReason && (
        <p className="text-xs italic text-muted-foreground">
          {decisionUnavailable
            ? tCard('decisionUnavailableReason')
            : policyReasonKey
              ? tCard(policyReasonKey)
              : tCard('rejectedReason', { reason: tool.rejectReason })}
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

  if (toolName.includes('send') || toolName.includes('post') || toolName.includes('reply')) {
    if (r.ok === true || r.sent === true || r.posted === true) return t('sent');
  }

  return null;
}
