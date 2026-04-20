'use client';

import { Info } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Switch } from '@/components/ui/switch';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip';
import { toolLabel, toolUserDescription, type Tool, type ToolApprovalValue } from '@/lib/schemas';

interface ToolApprovalToggleProps {
  tool: Tool;
  value: ToolApprovalValue;
  onChange: (value: ToolApprovalValue) => void;
  disabled?: boolean;
}

// ToolApprovalToggle renders a single tool row on /settings/tools.
//
//   - manual-floor → an «Автоматически» / «Вручную» Switch (data-state mapped
//     from the value so Radix + jsdom tests can read it).
//   - forbidden-floor → read-only «Запрещено» badge with an info tooltip
//     («Этот инструмент нельзя разрешить настройками»). Forbidden is a
//     registration-time property (POLICY-01) and must never be settable.
//   - auto-floor tools are filtered OUT by the parent page because they
//     have nothing to configure.
export function ToolApprovalToggle({
  tool,
  value,
  onChange,
  disabled = false,
}: ToolApprovalToggleProps) {
  const isForbidden = tool.floor === 'forbidden';
  const checked = value === 'auto';
  const label = toolLabel(tool);
  const userDesc = toolUserDescription(tool);

  return (
    <div className="flex items-start justify-between gap-4 rounded-md border p-3">
      <div className="min-w-0 flex-1">
        <p className="text-sm font-medium">{label}</p>
        {userDesc && <p className="mt-1 text-xs text-muted-foreground">{userDesc}</p>}
      </div>
      {isForbidden ? (
        <TooltipProvider delayDuration={100}>
          <Tooltip>
            <TooltipTrigger asChild>
              <span className="inline-flex items-center gap-1">
                <Badge variant="destructive">Запрещено</Badge>
                <Info className="h-3.5 w-3.5 text-muted-foreground" aria-hidden="true" />
                <span className="sr-only">Этот инструмент нельзя разрешить настройками</span>
              </span>
            </TooltipTrigger>
            <TooltipContent>Этот инструмент нельзя разрешить настройками</TooltipContent>
          </Tooltip>
        </TooltipProvider>
      ) : (
        <div className="flex shrink-0 items-center gap-2">
          <span
            className={
              checked ? 'text-xs text-muted-foreground' : 'text-xs font-medium text-foreground'
            }
          >
            Вручную
          </span>
          <Switch
            checked={checked}
            onCheckedChange={(next) => onChange(next ? 'auto' : 'manual')}
            disabled={disabled}
            aria-label={`Режим одобрения для ${label}`}
          />
          <span
            className={
              checked ? 'text-xs font-medium text-foreground' : 'text-xs text-muted-foreground'
            }
          >
            Автоматически
          </span>
        </div>
      )}
    </div>
  );
}
