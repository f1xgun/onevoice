'use client';

import { useEffect, useState } from 'react';
import { ChevronDown } from 'lucide-react';
import { Checkbox } from '@/components/ui/checkbox';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible';
import { cn } from '@/lib/utils';
import { PLATFORM_COLORS, PLATFORM_FULL_LABELS } from '@/lib/platforms';
import { useTools, groupByPlatform, type PlatformKey } from '@/lib/hooks/useTools';

interface ToolCheckboxGridProps {
  activeIntegrations: string[];
  value: string[];
  onChange: (allowed: string[]) => void;
}

const STORAGE_PREFIX = 'projects:whitelistPanel:';

function humanToolName(toolId: string): string {
  const parts = toolId.split('__');
  const action = parts[1] ?? toolId;
  return action.replace(/_/g, ' ');
}

function platformLabel(platform: string): string {
  return PLATFORM_FULL_LABELS[platform] ?? platform;
}

function readPersistedOpen(platform: string): boolean | undefined {
  if (typeof window === 'undefined') return undefined;
  try {
    const raw = window.localStorage.getItem(`${STORAGE_PREFIX}${platform}`);
    if (raw === null) return undefined;
    return raw === 'true';
  } catch {
    return undefined;
  }
}

function writePersistedOpen(platform: string, open: boolean) {
  if (typeof window === 'undefined') return;
  try {
    window.localStorage.setItem(`${STORAGE_PREFIX}${platform}`, String(open));
  } catch {
    // ignore storage errors (quota, disabled, etc.)
  }
}

function PlatformSection({
  platform,
  tools,
  value,
  onChange,
}: {
  platform: string;
  tools: string[];
  value: string[];
  onChange: (allowed: string[]) => void;
}) {
  const color = PLATFORM_COLORS[platform] ?? '#6b7280';
  const checkedInPlatform = tools.filter((t) => value.includes(t)).length;
  const [open, setOpen] = useState<boolean>(true);

  useEffect(() => {
    const persisted = readPersistedOpen(platform);
    if (persisted !== undefined) setOpen(persisted);
  }, [platform]);

  const handleOpenChange = (next: boolean) => {
    setOpen(next);
    writePersistedOpen(platform, next);
  };

  const toggleTool = (toolId: string, checked: boolean) => {
    if (checked) {
      if (!value.includes(toolId)) onChange([...value, toolId]);
    } else {
      onChange(value.filter((t) => t !== toolId));
    }
  };

  return (
    <Collapsible
      open={open}
      onOpenChange={handleOpenChange}
      className="rounded-md border bg-card"
      style={{ borderLeftColor: color, borderLeftWidth: 3 }}
    >
      <CollapsibleTrigger
        className="flex w-full items-center justify-between px-4 py-3 text-left hover:bg-muted/50"
        type="button"
      >
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium">{platformLabel(platform)}</span>
          <span className="text-xs text-muted-foreground">
            {checkedInPlatform} / {tools.length}
          </span>
        </div>
        <ChevronDown
          size={16}
          className={cn('transition-transform text-muted-foreground', open && 'rotate-180')}
        />
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className="space-y-3 px-4 pb-4">
          {tools.map((toolId) => {
            const id = `tool-${toolId}`;
            const checked = value.includes(toolId);
            const hintId = `${id}-hint`;
            return (
              <div key={toolId} className="flex items-start gap-3">
                <Checkbox
                  id={id}
                  checked={checked}
                  onCheckedChange={(v) => toggleTool(toolId, v === true)}
                  aria-describedby={hintId}
                />
                <div className="flex-1">
                  <label htmlFor={id} className="text-sm capitalize cursor-pointer">
                    {humanToolName(toolId)}
                  </label>
                  <p id={hintId} className="font-mono text-xs text-muted-foreground">
                    {toolId}
                  </p>
                </div>
              </div>
            );
          })}
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
}

// Platform ordering displayed to the user — matches the legacy Phase 15
// catalogue's key order so UAT-approved screenshots remain pixel-stable.
const PLATFORM_DISPLAY_ORDER: PlatformKey[] = [
  'telegram',
  'vk',
  'yandex_business',
  'google_business',
];

export function ToolCheckboxGrid({ activeIntegrations, value, onChange }: ToolCheckboxGridProps) {
  const { data: tools, isLoading } = useTools();

  // Show all registered platforms (not just the user's active integrations)
  // so a user can pre-configure a project before connecting a platform —
  // matches Phase 15 behaviour. If the user has zero integrations, still show
  // the platforms but render a helpful message instead of the grid below.
  if (activeIntegrations.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        Активных интеграций нет — доступных инструментов тоже нет.
      </p>
    );
  }

  if (isLoading || !tools) {
    return (
      <p className="text-sm text-muted-foreground">Загрузка списка инструментов…</p>
    );
  }

  const buckets = groupByPlatform(tools);
  const platforms = PLATFORM_DISPLAY_ORDER.filter((p) => buckets[p].length > 0);

  return (
    <div className="space-y-3">
      {platforms.map((platform) => (
        <PlatformSection
          key={platform}
          platform={platform}
          tools={buckets[platform].map((t) => t.name)}
          value={value}
          onChange={onChange}
        />
      ))}
    </div>
  );
}
