'use client';

import { useEffect, useState } from 'react';
import { ChevronDown, ChevronRight } from 'lucide-react';
import { useTranslations } from 'next-intl';

import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible';
import { AppTextarea as Textarea } from '@/components/design-system/AppInput';
import { cn } from '@/lib/utils';
import { PLATFORM_LABELS, getPlatform } from '@/lib/platforms';
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
  // Every tool in the approval card is a mutation the owner is being asked to
  // approve, so the args (the text/caption to be published) are shown expanded
  // by default — approving a collapsed card blind is the defect this avoids.
  const [open, setOpen] = useState(true);

  useEffect(() => {
    if (draft.decision === 'edit' || draft.decision === 'reject') {
      setOpen(true);
    }
  }, [draft.decision]);

  const platform = getPlatform(call.toolName);
  const label = PLATFORM_LABELS[platform] ?? platform.toUpperCase();

  const counterOver = draft.rejectReason.length > REJECT_REASON_MAX_LEN;
  const triggerLabel = open ? t('triggerCollapse') : t('triggerExpand');

  return (
    <div
      data-approval-call={call.callId}
      className={cn('min-w-0 border-b border-line', amberHighlighted && 'ring-2 ring-warning')}
    >
      {amberHighlighted && (
        <p role="alert" className="p-3 text-meta text-warning">
          {t('card.submitHelper')}
        </p>
      )}
      <Collapsible open={open} onOpenChange={setOpen}>
        <div className="flex flex-wrap items-center gap-2 px-3 py-2">
          <CollapsibleTrigger
            aria-label={`${call.toolName} — ${triggerLabel}`}
            className="inline-flex min-h-11 min-w-11 items-center justify-center text-ink"
          >
            {open ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
          </CollapsibleTrigger>
          <span className="rounded bg-paper-sunken px-2 py-1 text-meta font-medium text-ink">
            {label}
          </span>
          <span className="min-w-0 break-all font-mono text-technical text-ink-soft">
            {call.toolName}
          </span>
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
