import type { ToolCall } from '@/types/chat';
import { PLATFORM_COLORS, PLATFORM_LABELS, getPlatform } from '@/lib/platforms';

export function ToolCard({ tool }: { tool: ToolCall }) {
  const platform = getPlatform(tool.name);
  const color = PLATFORM_COLORS[platform] ?? '#6b7280';
  const label = PLATFORM_LABELS[platform] ?? platform.toUpperCase();

  return (
    <div
      className="space-y-1 rounded-md border p-3 text-sm"
      style={{ borderLeftColor: color, borderLeftWidth: 3 }}
    >
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <span
            className="rounded px-1.5 py-0.5 text-xs font-bold text-white"
            style={{ backgroundColor: color }}
          >
            {label}
          </span>
          <span className="font-mono text-xs text-gray-600">{tool.name}</span>
        </div>
        {tool.status === 'pending' && (
          <span className="h-4 w-4 animate-spin rounded-full border-2 border-gray-300 border-t-blue-500" />
        )}
        {tool.status === 'done' && <span className="text-green-500">✅</span>}
        {tool.status === 'error' && <span className="text-red-500">❌</span>}
        {tool.status === 'aborted' && (
          <span className="text-gray-500" title="Выполнение прервано — результат не получен">
            ⏸
          </span>
        )}
      </div>
      {tool.result && (
        <p className="truncate text-xs text-gray-500">{JSON.stringify(tool.result).slice(0, 80)}</p>
      )}
      {tool.error && <p className="text-xs text-red-500">{tool.error}</p>}
      {tool.status === 'aborted' && (
        <p className="text-xs italic text-gray-500">Выполнение прервано — результат не получен</p>
      )}
    </div>
  );
}
