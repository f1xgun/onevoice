'use client';

import { Info } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip';
import { cn } from '@/lib/utils';
import { toolLabel, toolUserDescription, type Tool, type ToolApprovalValue } from '@/lib/schemas';

interface ToolApprovalToggleProps {
  tool: Tool;
  value: ToolApprovalValue;
  onChange: (value: ToolApprovalValue) => void;
  disabled?: boolean;
}

// 2-mode segmented control matching the visual language of
// `<ApprovalSwitch>`. The full 4-mode primitive lives at
// components/ui/approval-switch.tsx; we only wire two segments here
// because the backend tool-approval contract is `manual | auto` —
// `off` and `auto-with-review` will land when the contract grows.
//
//   - manual-floor → segmented (Вручную / Автоматически)
//   - forbidden-floor → read-only «Запрещено» badge with info tooltip.
//     Forbidden is a registration-time property (POLICY-01) and must
//     never be user-settable.
//   - auto-floor tools are filtered OUT by the parent page.
export function ToolApprovalToggle({
  tool,
  value,
  onChange,
  disabled = false,
}: ToolApprovalToggleProps) {
  const isForbidden = tool.floor === 'forbidden';
  const label = toolLabel(tool);
  const userDesc = toolUserDescription(tool);

  return (
    <div className="flex flex-col items-stretch gap-3 rounded-md border border-line-soft bg-paper px-4 py-3 sm:flex-row sm:items-start sm:justify-between sm:gap-4">
      <div className="min-w-0 flex-1">
        <p className="text-sm font-medium text-ink">{label}</p>
        {userDesc && <p className="mt-1 text-xs leading-relaxed text-ink-mid">{userDesc}</p>}
      </div>
      {isForbidden ? (
        <TooltipProvider delayDuration={100}>
          <Tooltip>
            <TooltipTrigger asChild>
              <span className="inline-flex items-center gap-1">
                <Badge tone="danger">Запрещено</Badge>
                <Info className="h-3.5 w-3.5 text-ink-soft" aria-hidden="true" />
                <span className="sr-only">Этот инструмент нельзя разрешить настройками</span>
              </span>
            </TooltipTrigger>
            <TooltipContent>Этот инструмент нельзя разрешить настройками</TooltipContent>
          </Tooltip>
        </TooltipProvider>
      ) : (
        <ApprovalSegmented
          value={value}
          onChange={onChange}
          disabled={disabled}
          aria-label={`Режим одобрения для ${label}`}
        />
      )}
    </div>
  );
}

function ApprovalSegmented({
  value,
  onChange,
  disabled,
  'aria-label': ariaLabel,
}: {
  value: ToolApprovalValue;
  onChange: (next: ToolApprovalValue) => void;
  disabled?: boolean;
  'aria-label'?: string;
}) {
  const opts: { id: ToolApprovalValue; label: string }[] = [
    { id: 'manual', label: 'С вашего согласия' },
    { id: 'auto', label: 'Сам' },
  ];
  return (
    <div
      role="radiogroup"
      aria-label={ariaLabel}
      aria-disabled={disabled || undefined}
      className={cn(
        'inline-flex shrink-0 gap-0.5 rounded-md bg-paper-sunken p-0.5',
        disabled && 'opacity-50'
      )}
    >
      {opts.map((o) => {
        const selected = value === o.id;
        return (
          <button
            key={o.id}
            type="button"
            role="radio"
            aria-checked={selected}
            tabIndex={selected ? 0 : -1}
            disabled={disabled}
            onClick={() => onChange(o.id)}
            className={cn(
              'rounded-sm px-3 py-1 text-xs font-medium transition-colors',
              'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1',
              selected
                ? 'border border-line bg-paper text-ink shadow-ov-1'
                : 'border border-transparent text-ink-mid hover:text-ink',
              disabled && 'cursor-not-allowed'
            )}
          >
            {o.label}
          </button>
        );
      })}
    </div>
  );
}
