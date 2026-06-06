'use client';

import { useEffect, useState } from 'react';
import { ChevronDown, ChevronRight } from 'lucide-react';
import { useTranslations } from 'next-intl';

import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible';
import { Textarea } from '@/components/ui/textarea';
import { cn } from '@/lib/utils';
import { PLATFORM_COLORS, PLATFORM_LABELS, getPlatform } from '@/lib/platforms';
import type { ApprovalAction, PendingApprovalCall } from '@/types/chat';

import { ToolApprovalArgsForm } from './ToolApprovalArgsForm';
import { ToolApprovalToggleGroup } from './ToolApprovalToggleGroup';
import { REJECT_REASON_MAX_LEN } from './toolApprovalConstants';

type Decision = ApprovalAction | 'undecided';

/**
 * Minimal per-call draft shape consumed by the accordion entry. The root
 * `ToolApprovalCard` owns the full CallDraft (including batch-level
 * `amberHighlighted` + reducer plumbing) and projects only what this entry
 * needs. Keeping the shape narrow lets the entry test-mount without the
 * card reducer.
 */
export interface AccordionEntryDraft {
  decision: Decision;
  editedArgs: Record<string, string | number | boolean>;
  rejectReason: string;
}

export interface ToolApprovalAccordionEntryProps {
  call: PendingApprovalCall;
  draft: AccordionEntryDraft;
  disabled: boolean;
  amberHighlighted: boolean;
  onSelectDecision: (action: ApprovalAction) => void;
  onEditArg: (key: string, value: string | number | boolean) => void;
  onSetRejectReason: (reason: string) => void;
}

export function ToolApprovalAccordionEntry({
  call,
  draft,
  disabled,
  amberHighlighted,
  onSelectDecision,
  onEditArg,
  onSetRejectReason,
}: ToolApprovalAccordionEntryProps) {
  const t = useTranslations('chat.toolApproval');
  const [open, setOpen] = useState(false);

  useEffect(() => {
    if (draft.decision === 'edit' || draft.decision === 'reject') {
      setOpen(true);
    }
  }, [draft.decision]);

  const platform = getPlatform(call.toolName);
  const color = PLATFORM_COLORS[platform] ?? '#6b7280';
  const label = PLATFORM_LABELS[platform] ?? platform.toUpperCase();

  const counterOver = draft.rejectReason.length > REJECT_REASON_MAX_LEN;
  const triggerLabel = open ? t('triggerCollapse') : t('triggerExpand');

  return (
    <div
      className={cn('rounded-md border', amberHighlighted && 'ring-2 ring-amber-400')}
      style={{ borderLeftColor: color, borderLeftWidth: 3 }}
    >
      <Collapsible open={open} onOpenChange={setOpen}>
        <div className="flex flex-wrap items-center gap-2 px-3 py-2">
          <CollapsibleTrigger
            aria-label={`${call.toolName} — ${triggerLabel}`}
            className="inline-flex items-center text-ink-mid hover:text-ink"
          >
            {open ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
          </CollapsibleTrigger>
          <span
            className="rounded px-1.5 py-0.5 text-xs font-bold text-paper"
            style={{ backgroundColor: color }}
          >
            {label}
          </span>
          <span className="font-mono text-xs text-ink-mid">{call.toolName}</span>
        </div>

        <div className="px-3 pb-3">
          <ToolApprovalToggleGroup
            toolName={call.toolName}
            decision={draft.decision}
            disabled={disabled}
            onSelect={onSelectDecision}
          />
        </div>

        <CollapsibleContent>
          {/*
            The "Параметры" block is always visible when the entry is
            expanded — operators can inspect args before picking a decision.
            The form below switches between editable (decision === 'edit')
            and read-only context-list modes; the rest of the wrapper stays
            stable so the layout does not jump on decision toggle.
          */}
          <div className="space-y-2 px-3 pb-3">
            <p className="text-sm font-semibold">{t('argsHeading')}</p>
            <ToolApprovalArgsForm
              args={call.args}
              editedArgs={draft.editedArgs}
              editableFields={call.editableFields}
              editable={draft.decision === 'edit'}
              disabled={disabled}
              onEdit={onEditArg}
            />
          </div>

          {draft.decision === 'reject' && (
            <div className="space-y-1 px-3 pb-3">
              <Textarea
                placeholder={t('rejectPlaceholder')}
                aria-label={t('rejectAriaLabel')}
                value={draft.rejectReason}
                maxLength={500}
                disabled={disabled}
                onChange={(e) => onSetRejectReason(e.target.value)}
              />
              <p
                aria-live="polite"
                className={cn(
                  'text-right text-xs',
                  counterOver ? 'text-destructive' : 'text-muted-foreground'
                )}
              >
                {/* eslint-disable-next-line i18next/no-literal-string -- pure numeric counter "<n> / <max>", no localizable copy. */}
                {draft.rejectReason.length} / {REJECT_REASON_MAX_LEN}
              </p>
            </div>
          )}
        </CollapsibleContent>
      </Collapsible>
    </div>
  );
}
