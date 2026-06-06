'use client';

import { useEffect, useState } from 'react';
import { ChevronDown } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { Checkbox } from '@/components/ui/checkbox';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible';
import { cn } from '@/lib/utils';
import { PLATFORM_COLORS, usePlatformFullLabels } from '@/lib/platforms';
import { useTools, groupByPlatform, TOOL_PLATFORM_ORDER } from '@/lib/hooks/useTools';
import { toolLabel, toolUserDescription, type Tool } from '@/lib/schemas';

interface ToolCheckboxGridProps {
  activeIntegrations: string[];
  value: string[];
  onChange: (allowed: string[]) => void;
}

const STORAGE_PREFIX = 'projects:whitelistPanel:';

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
  } catch {}
}

function PlatformSection({
  platform,
  platformLabel,
  tools,
  value,
  onChange,
}: {
  platform: string;
  platformLabel: string;
  tools: Tool[];
  value: string[];
  onChange: (allowed: string[]) => void;
}) {
  const color = PLATFORM_COLORS[platform] ?? '#6b7280';
  const checkedInPlatform = tools.filter((t) => value.includes(t.name)).length;
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
        className="hover:bg-muted/50 flex w-full items-center justify-between px-4 py-3 text-left"
        type="button"
      >
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium">{platformLabel}</span>
          <span className="text-xs text-muted-foreground">
            {checkedInPlatform} / {tools.length}
          </span>
        </div>
        <ChevronDown
          size={16}
          className={cn('text-muted-foreground transition-transform', open && 'rotate-180')}
        />
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className="space-y-3 px-4 pb-4">
          {tools.map((tool) => {
            const id = `tool-${tool.name}`;
            const checked = value.includes(tool.name);
            return (
              <div key={tool.name} className="flex items-start gap-3">
                <Checkbox
                  id={id}
                  checked={checked}
                  onCheckedChange={(v) => toggleTool(tool.name, v === true)}
                />
                <div className="flex-1">
                  <label htmlFor={id} className="cursor-pointer text-sm font-medium">
                    {toolLabel(tool)}
                  </label>
                  {toolUserDescription(tool) && (
                    <p className="mt-0.5 text-xs text-muted-foreground">
                      {toolUserDescription(tool)}
                    </p>
                  )}
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
  const tToolGrid = useTranslations('projects.toolCheckboxes');
  const platformFullLabels = usePlatformFullLabels();
  const { data: tools, isLoading } = useTools();

  if (activeIntegrations.length === 0) {
    return <p className="text-sm text-muted-foreground">{tToolGrid('noIntegrations')}</p>;
  }

  if (isLoading || !tools) {
    return <p className="text-sm text-muted-foreground">{tToolGrid('loading')}</p>;
  }

  const buckets = groupByPlatform(tools);
  const platforms = TOOL_PLATFORM_ORDER.filter((p) => buckets[p].length > 0);

  return (
    <div className="space-y-3">
      {platforms.map((platform) => (
        <PlatformSection
          key={platform}
          platform={platform}
          platformLabel={platformFullLabels[platform] ?? platform}
          tools={buckets[platform]}
          value={value}
          onChange={onChange}
        />
      ))}
    </div>
  );
}
