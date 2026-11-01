'use client';

import { useEffect, useState } from 'react';
import { ChevronDown, ChevronRight } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { format } from 'date-fns';
import { AppInput } from '@/components/design-system/AppInput';
import { ActionButton } from '@/components/design-system/ActionButton';
import {
  Sheet,
  SheetContent,
  SheetTitle,
  SheetDescription,
} from '@/components/design-system/AppSheet';

import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible';
import { AppTextarea as Textarea } from '@/components/design-system/AppInput';
import { cn } from '@/lib/utils';
import { approvalKind } from '@/lib/approvalTelemetry';
import { useApprovalImpressions } from '@/hooks/useApprovalImpressions';
import { getPlatform, usePlatformFullLabels } from '@/lib/platforms';
import type { ApprovalAction, PendingApprovalCall } from '@/types/chat';

import { PlatformIcon } from '@/components/integrations/PlatformIcons';
import { ChatPhotoInput } from './ChatPhotoInput';
import { ToolApprovalArgsForm } from './ToolApprovalArgsForm';
import { ToolApprovalToggleGroup } from './ToolApprovalToggleGroup';
import { REJECT_REASON_MAX_LEN } from './toolApprovalConstants';
import { useToolDisplayName } from './useToolDisplayName';

const timeSchema = z.object({
  when: z
    .string()
    .min(1)
    .refine(
      (value) =>
        Number.isFinite(new Date(value).getTime()) && new Date(value).getTime() > Date.now()
    ),
});

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
  batchId?: string;
  draft: AccordionEntryDraft;
  disabled: boolean;
  amberHighlighted: boolean;
  onSelectDecision: (action: ApprovalAction) => void;
  onEditArg: (key: string, value: string | number | boolean) => void;
  onSetRejectReason: (reason: string) => void;
  onUploadingChange?: (uploading: boolean) => void;
}

export function ToolApprovalAccordionEntry({
  call,
  batchId,
  draft,
  disabled,
  amberHighlighted,
  onSelectDecision,
  onEditArg,
  onSetRejectReason,
  onUploadingChange,
}: ToolApprovalAccordionEntryProps) {
  const t = useTranslations('chat.toolApproval');
  const platformLabels = usePlatformFullLabels();
  useApprovalImpressions(
    `${batchId}-${call.callId}`,
    approvalKind(call.toolName),
    'chat',
    !!batchId
  );
  // Every tool in the approval card is a mutation the owner is being asked to
  // approve, so the args (the text/caption to be published) are shown expanded
  // by default — approving a collapsed card blind is the defect this avoids.
  const [open, setOpen] = useState(true);
  const [photoName, setPhotoName] = useState<string | null>(null);
  const [timeOpen, setTimeOpen] = useState(false);
  const timeForm = useForm<z.infer<typeof timeSchema>>({
    resolver: zodResolver(timeSchema),
    defaultValues: { when: '' },
  });
  const canSchedule =
    ['telegram__send_channel_post', 'telegram__send_photo'].includes(call.toolName) &&
    call.editableFields.includes('scheduled_at');
  const scheduled = draft.editedArgs.scheduled_at ?? call.args.scheduled_at;
  const scheduledDate = typeof scheduled === 'string' && scheduled ? new Date(scheduled) : null;
  const validScheduled = scheduledDate && Number.isFinite(scheduledDate.getTime());
  function chooseTime(value: string) {
    onSelectDecision('edit');
    onEditArg('scheduled_at', value);
    setTimeOpen(false);
  }

  useEffect(() => {
    if (draft.decision === 'edit' || draft.decision === 'reject') {
      setOpen(true);
    }
  }, [draft.decision]);

  const platform = getPlatform(call.toolName);
  const platformLabel = platformLabels[platform] ?? t('unknownPlatform');
  const actionName = useToolDisplayName(call.toolName);

  const counterOver = draft.rejectReason.length > REJECT_REASON_MAX_LEN;
  const triggerLabel = open ? t('triggerCollapse') : t('triggerExpand');

  return (
    <div
      data-approval-call={call.callId}
      className={cn(
        'min-w-0 rounded-block bg-paper-raised p-1',
        amberHighlighted && 'ring-2 ring-warning'
      )}
    >
      {amberHighlighted && (
        <p role="alert" className="p-3 text-meta text-warning">
          {t('card.submitHelper')}
        </p>
      )}
      <Collapsible open={open} onOpenChange={setOpen}>
        <div className="flex flex-wrap items-center gap-2 px-3 py-2">
          <CollapsibleTrigger
            aria-label={`${actionName} — ${triggerLabel}`}
            className="inline-flex min-h-11 min-w-11 items-center justify-center text-ink"
          >
            {open ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
          </CollapsibleTrigger>
          <span className="rounded bg-paper-sunken px-2 py-1 text-meta font-medium text-ink">
            <PlatformIcon platform={platform} className="mr-1.5 inline-block size-4" />
            {platformLabel}
          </span>
          <span className="min-w-0 text-sm font-medium text-ink">{actionName}</span>
        </div>

        <div className="px-3 pb-3">
          <ToolApprovalToggleGroup
            actionName={actionName}
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
            {canSchedule && (
              <>
                <ActionButton
                  variant="secondary"
                  disabled={disabled}
                  onClick={() => {
                    timeForm.reset({
                      when: validScheduled ? format(scheduledDate, "yyyy-MM-dd'T'HH:mm") : '',
                    });
                    setTimeOpen(true);
                  }}
                >
                  {t('time.label')}:{' '}
                  {validScheduled
                    ? scheduledDate.toLocaleString()
                    : scheduled
                      ? t('time.invalid')
                      : t('time.now')}
                </ActionButton>
                <Sheet open={timeOpen} onOpenChange={setTimeOpen}>
                  <SheetContent side="bottom">
                    <SheetTitle>{t('time.title')}</SheetTitle>
                    <SheetDescription>{t('time.description')}</SheetDescription>
                    <ActionButton
                      variant="secondary"
                      disabled={disabled}
                      onClick={() => chooseTime('')}
                    >
                      {t('time.now')}
                    </ActionButton>
                    <form
                      onSubmit={timeForm.handleSubmit(({ when }) =>
                        chooseTime(new Date(when).toISOString())
                      )}
                      className="space-y-3"
                    >
                      <label className="block text-sm">
                        {t('time.choose')}
                        <AppInput
                          type="datetime-local"
                          aria-label={t('time.choose')}
                          {...timeForm.register('when')}
                        />
                      </label>
                      {timeForm.formState.errors.when && (
                        <p role="alert" className="text-sm text-danger">
                          {t('time.invalid')}
                        </p>
                      )}
                      <ActionButton type="submit" disabled={disabled}>
                        {t('time.save')}
                      </ActionButton>
                    </form>
                  </SheetContent>
                </Sheet>
              </>
            )}
            {draft.decision === 'edit' && call.editableFields.includes('photo_url') && (
              <ChatPhotoInput
                disabled={disabled}
                onUploadingChange={onUploadingChange}
                onChange={(photo) => {
                  setPhotoName(photo.name);
                  onEditArg('photo_url', photo.url);
                }}
              />
            )}
            {photoName && <p className="text-sm text-ink-soft">{photoName}</p>}
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
