'use client';

import { Check, Pencil, X } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { cn } from '@/lib/utils';
import type { ApprovalAction } from '@/types/chat';

type ActiveVariant = 'secondary' | 'destructive';

// Variant mapping for the active state of each action (UI-SPEC §Color §Copy).
// Approve → primary (indigo), Edit → secondary with an extra ring, Reject →
// destructive (red). Inactive state on every button is `outline`.
const ACTIVE_VARIANTS: Record<ApprovalAction, ActiveVariant> = {
  approve: 'secondary',
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

// Action → aria-label key (under chat.toolApproval.actions). Kept in a
// const so the test contract `aria-label="Одобрить telegram__send_..."`
// stays mechanical (renders as `t(ariaKey, { toolName })`).
const ARIA_LABEL_KEYS: Record<ApprovalAction, 'approveAria' | 'editAria' | 'rejectAria'> = {
  approve: 'approveAria',
  edit: 'editAria',
  reject: 'rejectAria',
};

function ToggleBtn({ action, active, disabled, toolName, icon: Icon, onClick }: ToggleBtnProps) {
  const tActions = useTranslations('chat.toolApproval.actions');
  const variant = active ? ACTIVE_VARIANTS[action] : 'outline';
  return (
    <Button
      variant={variant}
      size="sm"
      disabled={disabled}
      aria-pressed={active}
      aria-label={tActions(ARIA_LABEL_KEYS[action], { toolName })}
      onClick={onClick}
      className={cn('h-8 px-3', active && 'ring-2 ring-ring')}
    >
      <Icon size={14} className="mr-1" />
      {tActions(action)}
    </Button>
  );
}

/**
 * Mutually-exclusive three-button segmented control for the approval card.
 * Not a `radiogroup` — users can re-pick freely, so the WAI-ARIA pattern is
 * "buttons with aria-pressed", not RadioGroup. Parent owns the `decision`.
 */
export function ToolApprovalToggleGroup({
  toolName,
  decision,
  disabled,
  onSelect,
}: ToolApprovalToggleGroupProps) {
  const tActions = useTranslations('chat.toolApproval.actions');
  return (
    <div
      role="group"
      aria-label={tActions('groupAria', { toolName })}
      className="flex flex-wrap gap-2"
    >
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
