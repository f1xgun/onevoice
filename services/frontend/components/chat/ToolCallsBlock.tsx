'use client';

import { useEffect, useId, useState } from 'react';
import { ChevronDown, ChevronRight } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { ToolCard } from './ToolCard';
import type { ToolCall } from '@/types/chat';
import { PLATFORM_LABELS, getPlatform } from '@/lib/platforms';

function PlatformBadge({ name }: { name: string }) {
  const platform = getPlatform(name);
  return (
    <span className="rounded bg-paper-raised px-2 py-1 text-meta text-ink">
      {PLATFORM_LABELS[platform] ?? platform.toUpperCase()}
    </span>
  );
}

export function ToolCallsBlock({ toolCalls }: { toolCalls: ToolCall[] }) {
  const tCalls = useTranslations('chat.toolCalls');
  const contentId = useId();
  const [expanded, setExpanded] = useState(false);

  const doneCount = toolCalls.filter((t) => t.status === 'done').length;
  const failCount = toolCalls.filter((t) => t.status === 'error').length;
  const platforms = Array.from(new Set(toolCalls.map((t) => t.name.split('__')[0])));

  useEffect(() => {
    if (failCount > 0) setExpanded(true);
  }, [failCount]);

  if (toolCalls.length === 0) return null;

  return (
    <div className="mt-2 overflow-hidden rounded-md border border-line bg-paper-raised">
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
            ? tCalls('hideActions', { count: toolCalls.length })
            : tCalls('showActions', { count: toolCalls.length })}
        </span>
        <span className="ml-1 text-meta text-ink-soft">
          {tCalls('doneCount', { done: doneCount, total: toolCalls.length })}
        </span>
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

      {expanded && (
        <div id={contentId} className="space-y-2 p-2">
          {toolCalls.map((tool) => (
            <ToolCard key={tool.id} tool={tool} />
          ))}
        </div>
      )}
    </div>
  );
}
