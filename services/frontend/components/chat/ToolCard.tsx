import type { ToolCall } from '@/types/chat'
import { PLATFORM_COLORS, PLATFORM_LABELS, getPlatform } from '@/lib/platforms'

export function ToolCard({ tool }: { tool: ToolCall }) {
  const platform = getPlatform(tool.name)
  const color = PLATFORM_COLORS[platform] ?? '#6b7280'
  const label = PLATFORM_LABELS[platform] ?? platform.toUpperCase()

  return (
    <div className="border rounded-md p-3 text-sm space-y-1" style={{ borderLeftColor: color, borderLeftWidth: 3 }}>
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <span
            className="px-1.5 py-0.5 rounded text-white text-xs font-bold"
            style={{ backgroundColor: color }}
          >
            {label}
          </span>
          <span className="font-mono text-xs text-gray-600">{tool.name}</span>
        </div>
        {tool.status === 'pending' && (
          <span className="w-4 h-4 border-2 border-gray-300 border-t-blue-500 rounded-full animate-spin" />
        )}
        {tool.status === 'done' && <span className="text-green-500">✅</span>}
        {tool.status === 'error' && <span className="text-red-500">❌</span>}
      </div>
      {tool.result && (
        <p className="text-gray-500 text-xs truncate">
          {JSON.stringify(tool.result).slice(0, 80)}
        </p>
      )}
      {tool.error && <p className="text-red-500 text-xs">{tool.error}</p>}
    </div>
  )
}
