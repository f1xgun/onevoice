'use client'

import { useState } from 'react'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { ToolCard } from './ToolCard'
import type { ToolCall } from '@/types/chat'
import { PLATFORM_COLORS, PLATFORM_LABELS, getPlatform } from '@/lib/platforms'

function PlatformBadge({ name }: { name: string }) {
  const platform = getPlatform(name)
  const color = PLATFORM_COLORS[platform] ?? '#6b7280'
  return (
    <span
      className="px-1.5 py-0.5 rounded text-white text-xs font-bold"
      style={{ backgroundColor: color }}
    >
      {PLATFORM_LABELS[platform] ?? platform.toUpperCase()}
    </span>
  )
}

export function ToolCallsBlock({ toolCalls }: { toolCalls: ToolCall[] }) {
  const [expanded, setExpanded] = useState(false)
  if (toolCalls.length === 0) return null

  const doneCount = toolCalls.filter((t) => t.status === 'done').length
  const platforms = Array.from(new Set(toolCalls.map((t) => t.name.split('__')[0])))

  return (
    <div className="mt-2 border border-gray-200 rounded-md overflow-hidden">
      <button
        type="button"
        onClick={() => setExpanded((e) => !e)}
        className="w-full flex items-center gap-2 px-3 py-2 bg-gray-50 hover:bg-gray-100 text-sm text-left"
      >
        {expanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
        <span className="text-gray-600">
          {expanded ? 'Скрыть' : 'Показать'} действия ({toolCalls.length})
        </span>
        <span className="text-green-600 text-xs ml-1">✓ {doneCount}/{toolCalls.length}</span>
        <div className="flex gap-1 ml-auto">
          {platforms.map((p) => <PlatformBadge key={p} name={p + '__x'} />)}
        </div>
      </button>

      {expanded && (
        <div className="p-2 space-y-2 bg-white">
          {toolCalls.map((tool) => (
            <ToolCard key={tool.id} tool={tool} />
          ))}
        </div>
      )}
    </div>
  )
}
