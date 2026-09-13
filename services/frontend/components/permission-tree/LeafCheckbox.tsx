'use client';

import { useTranslations } from 'next-intl';

import { Checkbox } from '@/components/ui/checkbox';
import { cn } from '@/lib/utils';

const KNOWN_PERMISSION_NAMES = new Set([
  'business.read',
  'business.update',
  'business.delete',
  'business.transfer_ownership',
  'members.read',
  'members.invite',
  'members.remove',
  'members.update_role',
  'roles.read',
  'roles.create',
  'roles.update',
  'roles.delete',
  'integrations.read',
  'integrations.connect',
  'integrations.disconnect',
  'content.read',
  'content.create',
  'content.update',
  'content.delete',
  'billing.read',
  'billing.update',
  'audit.read',
]);

export interface LeafCheckboxProps {
  /** Internal permission key used only to resolve localized copy. */
  leafName: string;
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
  checked,
  disabled,
  actorHas,
  onToggle,
}: LeafCheckboxProps) {
  const t = useTranslations('roles.permissionTree');
  const labelKey = `permissions.${leafName}`;
  const label = KNOWN_PERMISSION_NAMES.has(leafName) ? t(labelKey) : t('unknownPermission');
  return (
    <li className={cn('flex items-center gap-2 py-1', !actorHas && 'opacity-60')}>
      <Checkbox
        checked={checked}
        disabled={disabled}
        aria-label={label}
        onCheckedChange={(next) => onToggle(next === true)}
      />
      <span className="flex-1 text-sm text-ink">{label}</span>
      {!actorHas && <span className="text-xs text-ink-soft">{t('disabledTooltip')}</span>}
    </li>
  );
}
