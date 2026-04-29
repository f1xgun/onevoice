'use client';

import { useState } from 'react';
import { ChevronDown, ChevronRight } from 'lucide-react';
import { ToolCard } from './ToolCard';
import type { ToolCall } from '@/types/chat';
import { PLATFORM_COLORS, PLATFORM_LABELS, getPlatform } from '@/lib/platforms';

function PlatformBadge({ name }: { name: string }) {
  const platform = getPlatform(name);
  const color = PLATFORM_COLORS[platform] ?? '#6b7280';
  return (
    <span
      className="rounded px-1.5 py-0.5 text-xs font-bold text-paper"
      style={{ backgroundColor: color }}
    >
      {PLATFORM_LABELS[platform] ?? platform.toUpperCase()}
    </span>
  );
}

export function ToolCallsBlock({ toolCalls }: { toolCalls: ToolCall[] }) {
  const [expanded, setExpanded] = useState(false);
  if (toolCalls.length === 0) return null;

  const doneCount = toolCalls.filter((t) => t.status === 'done').length;
  const platforms = Array.from(new Set(toolCalls.map((t) => t.name.split('__')[0])));

  return (
    <div className="mt-2 overflow-hidden rounded-md border border-line">
      <button
        type="button"
        onClick={() => setExpanded((e) => !e)}
        className="flex w-full items-center gap-2 bg-paper-sunken px-3 py-2 text-left text-sm hover:bg-paper-sunken"
      >
        {expanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
        <span className="text-ink-mid">
          {expanded ? 'Скрыть' : 'Показать'} действия ({toolCalls.length})
        </span>
        <span className="ml-1 text-xs text-[var(--ov-success)]">
          ✓ {doneCount}/{toolCalls.length}
        </span>
        <div className="ml-auto flex gap-1">
          {platforms.map((p) => (
            <PlatformBadge key={p} name={p + '__x'} />
          ))}
        </div>
      </button>

      {expanded && (
        <div className="space-y-2 bg-paper-raised p-2">
          {toolCalls.map((tool) => (
            <ToolCard key={tool.id} tool={tool} />
          ))}
        </div>
      )}
    </div>
  );
}
