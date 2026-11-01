'use client';

import { useEffect, useId, useState } from 'react';
import { ChevronDown, ChevronRight, Loader2 } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { ToolCard } from './ToolCard';
import type { ToolCall } from '@/types/chat';
import { PlatformIcon } from '@/components/integrations/PlatformIcons';
import { usePlatformFullLabels, getPlatform } from '@/lib/platforms';

function PlatformBadge({ name }: { name: string }) {
  const platform = getPlatform(name);
  const labels = usePlatformFullLabels();
  const tCard = useTranslations('chat.toolCard');
  return (
    <span className="inline-flex items-center gap-1.5 rounded bg-paper-raised px-2 py-1 text-meta text-ink">
      <PlatformIcon platform={platform} className="h-4 w-4" />
      {labels[platform] ?? tCard('unknownPlatform')}
    </span>
  );
}

export function ToolCallsBlock({ toolCalls }: { toolCalls: ToolCall[] }) {
  const tCalls = useTranslations('chat.toolCalls');
  const contentId = useId();
  const [expanded, setExpanded] = useState(false);

  const serviceCalls = toolCalls.filter(
    (tool) => /__(get|list|search|fetch|read)_/.test(tool.name) && tool.status !== 'error'
  );
  const actionCalls = toolCalls.filter((tool) => !serviceCalls.includes(tool));
  const doneCount = serviceCalls.filter((t) => t.status === 'done').length;
  const pendingCount = serviceCalls.filter((tool) => tool.status === 'pending').length;
  const failCount = serviceCalls.filter((t) => t.status === 'error').length;
  const platforms = Array.from(new Set(toolCalls.map((t) => t.name.split('__')[0])));

  useEffect(() => {
    if (failCount > 0) setExpanded(true);
  }, [failCount]);

  if (toolCalls.length === 0) return null;

  return (
    <div className="mt-2 overflow-hidden rounded-md border border-line bg-paper-raised">
      {serviceCalls.length > 0 && (
        <button
          type="button"
          aria-expanded={expanded}
          aria-controls={contentId}
          onClick={() => setExpanded((e) => !e)}
          className="flex min-h-11 w-full flex-wrap items-center gap-2 bg-paper-sunken px-3 py-2 text-left text-sm text-ink-mid transition-colors hover:bg-paper-well"
        >
          {expanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
          <span>
            {expanded
              ? tCalls('hideActions', { count: serviceCalls.length })
              : tCalls('showActions', { count: serviceCalls.length })}
          </span>
          <span className="ml-1 text-meta text-ink-soft">
            {tCalls('doneCount', { done: doneCount, total: serviceCalls.length })}
          </span>
          {pendingCount > 0 && (
            <span role="status" className="inline-flex items-center gap-1 text-meta text-info">
              <Loader2 aria-hidden className="h-4 w-4 animate-spin motion-reduce:animate-none" />
              {tCalls('runningCount', { count: pendingCount })}
            </span>
          )}
          {failCount > 0 && (
            <span className="ml-1 text-meta text-danger">
              {tCalls('failCount', { failed: failCount })}
            </span>
          )}
          <div className="ml-auto flex flex-wrap gap-1">
            {platforms.map((p) => (
              <PlatformBadge key={p} name={p + '__x'} />
            ))}
          </div>
        </button>
      )}

      {expanded && serviceCalls.length > 0 && (
        <div id={contentId} className="space-y-2 p-2">
          {serviceCalls.map((tool) => (
            <ToolCard key={tool.id} tool={tool} />
          ))}
        </div>
      )}
      {actionCalls.length > 0 && (
        <div className="space-y-3 p-2">
          {actionCalls.map((tool) => (
            <ToolCard key={tool.id} tool={tool} />
          ))}
        </div>
      )}
    </div>
  );
}
