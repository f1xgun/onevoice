'use client';

import { useEffect, useState } from 'react';
import { ChevronDown } from 'lucide-react';
import { Checkbox } from '@/components/ui/checkbox';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible';
import { cn } from '@/lib/utils';
import { PLATFORM_COLORS, PLATFORM_FULL_LABELS } from '@/lib/platforms';
import { TOOLS_BY_PLATFORM } from '@/lib/tools-catalogue';

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

export function ToolCheckboxGrid({ activeIntegrations, value, onChange }: ToolCheckboxGridProps) {
  // Show all platforms that have tools in the catalogue, so a user can
  // pre-configure a project before connecting a platform. If the user has
  // zero integrations, show a helpful message but still render the full list.
  const platforms = Object.keys(TOOLS_BY_PLATFORM).filter(
    (p) => TOOLS_BY_PLATFORM[p] && (TOOLS_BY_PLATFORM[p] as string[]).length > 0
  );

  if (activeIntegrations.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        Активных интеграций нет — доступных инструментов тоже нет.
      </p>
    );
  }

  return (
    <div className="space-y-3">
      {platforms.map((platform) => (
        <PlatformSection
          key={platform}
          platform={platform}
          tools={TOOLS_BY_PLATFORM[platform] ?? []}
          value={value}
          onChange={onChange}
        />
      ))}
    </div>
  );
}
