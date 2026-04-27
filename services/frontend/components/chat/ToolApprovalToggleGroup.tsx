'use client';

import { Check, Pencil, X } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';
import type { ApprovalAction } from '@/types/chat';

// Exact Russian labels — UI-SPEC §Copywriting Contract. Do NOT paraphrase.
const RU_LABELS = {
  approve: 'Одобрить',
  edit: 'Изменить',
  reject: 'Отклонить',
} as const;

type ActiveVariant = 'default' | 'secondary' | 'destructive';

// Variant mapping for the active state of each action (UI-SPEC §Color §Copy).
// Approve → primary (indigo), Edit → secondary with an extra ring, Reject →
// destructive (red). Inactive state on every button is `outline`.
const ACTIVE_VARIANTS: Record<ApprovalAction, ActiveVariant> = {
  approve: 'default',
  edit: 'secondary',
  reject: 'destructive',
};

export interface ToolApprovalToggleGroupProps {
  /** Used in aria-label for every button — mandatory for screen readers. */
  toolName: string;
  /** Current draft decision; 'undecided' renders all three buttons inactive. */
  decision: ApprovalAction | 'undecided';
  /** When true, propagates `disabled` to every button (resolve-in-flight UX). */
  disabled?: boolean;
  /** Parent-owned reducer dispatch; called with the clicked action. */
  onSelect: (action: ApprovalAction) => void;
}

interface ToggleBtnProps {
  action: ApprovalAction;
  active: boolean;
  disabled?: boolean;
  toolName: string;
  icon: typeof Check;
  onClick: () => void;
}

function ToggleBtn({ action, active, disabled, toolName, icon: Icon, onClick }: ToggleBtnProps) {
  const variant = active ? ACTIVE_VARIANTS[action] : 'outline';
  return (
    <Button
      variant={variant}
      size="sm"
      disabled={disabled}
      aria-pressed={active}
      aria-label={`${RU_LABELS[action]} ${toolName}`}
      onClick={onClick}
      className={cn(
        'h-8 px-3',
        // Edit's active state needs an extra ring (UI-SPEC — "neutral,
        // distinguishable from Approve's primary fill").
        active && action === 'edit' && 'ring-2 ring-ring',
        // Dim inactive siblings (UI-SPEC — "opacity-60 hover:opacity-100").
        !active && 'opacity-60 hover:opacity-100'
      )}
    >
      <Icon size={14} className="mr-1" />
      {RU_LABELS[action]}
    </Button>
  );
}

/**
 * Mutually-exclusive three-button segmented control for the approval card.
 * Each call gets one of these. Not a `radiogroup` — users can re-pick among
 * independent toggle buttons (D-07), so the WAI-ARIA pattern here is
 * "buttons with aria-pressed", not RadioGroup. Parent owns the `decision`.
 */
export function ToolApprovalToggleGroup({
  toolName,
  decision,
  disabled,
  onSelect,
}: ToolApprovalToggleGroupProps) {
  return (
    <div role="group" aria-label={`Действия для ${toolName}`} className="flex flex-wrap gap-2">
      <ToggleBtn
        action="approve"
        active={decision === 'approve'}
        disabled={disabled}
        toolName={toolName}
        icon={Check}
        onClick={() => onSelect('approve')}
      />
      <ToggleBtn
        action="edit"
        active={decision === 'edit'}
        disabled={disabled}
        toolName={toolName}
        icon={Pencil}
        onClick={() => onSelect('edit')}
      />
      <ToggleBtn
        action="reject"
        active={decision === 'reject'}
        disabled={disabled}
        toolName={toolName}
        icon={X}
        onClick={() => onSelect('reject')}
      />
    </div>
  );
}
