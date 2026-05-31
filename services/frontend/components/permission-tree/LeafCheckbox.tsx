'use client';

import { Info } from 'lucide-react';
import { useTranslations } from 'next-intl';

import { Checkbox } from '@/components/ui/checkbox';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import { cn } from '@/lib/utils';

export interface LeafCheckboxProps {
  /** Permission key — comes from the catalog (e.g. catalog[i].permissions[j].name). */
  leafName: string;
  /** Russian description — comes from the catalog ( filled these in pkg/authz). */
  description: string;
  /** Whether this leaf is currently selected in the form value. */
  checked: boolean;
  /** Form-level disable OR actor lacks the permission. */
  disabled: boolean;
  /** True iff the actor holds this permission — drives tooltip copy + opacity. */
  actorHas: boolean;
  /** Fired when the user toggles the checkbox. Never fires when `disabled`. */
  onToggle: (checked: boolean) => void;
}

/**
 * One permission leaf — checkbox + monospace permission name + Info icon tooltip.
 *
 * Disabled leaves (actor lacks the permission) render with `opacity-60`
 * and show «У вас нет этого права» on tooltip hover/focus. Radix Checkbox
 * in disabled state removes itself from tab order; the Info icon has
 * `tabIndex=0` so keyboard users can still focus the description.
 *
 * Holds NO hardcoded permission keys — every leaf name comes from the catalog.
 *
 * Known a11y limitation: trigger's `aria-label` and TooltipContent render
 * the same text, so screen readers announce the description twice. A proper
 * fix would lift the description into `aria-describedby` on the checkbox.
 */
export function LeafCheckbox({
  leafName,
  description,
  checked,
  disabled,
  actorHas,
  onToggle,
}: LeafCheckboxProps) {
  const t = useTranslations('roles.permissionTree');
  const tooltipText = actorHas ? description : t('disabledTooltip');

  return (
    <li className={cn('flex items-center gap-2 py-1', !actorHas && 'opacity-60')}>
      <Checkbox
        checked={checked}
        disabled={disabled}
        aria-label={leafName}
        onCheckedChange={(next) => onToggle(next === true)}
      />
      <span className="flex-1 font-mono text-sm text-ink">{leafName}</span>
      <Tooltip>
        <TooltipTrigger asChild>
          <span
            aria-label={tooltipText}
            tabIndex={0}
            className="inline-flex h-4 w-4 items-center justify-center text-ink-soft focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
          >
            <Info size={14} aria-hidden="true" />
          </span>
        </TooltipTrigger>
        <TooltipContent>{tooltipText}</TooltipContent>
      </Tooltip>
    </li>
  );
}
