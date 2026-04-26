'use client';

import { useEffect, useState } from 'react';
import { ChevronDown, ChevronRight } from 'lucide-react';
import JsonView from '@uiw/react-json-view';

import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible';
import { Textarea } from '@/components/ui/textarea';
import { cn } from '@/lib/utils';
import { PLATFORM_COLORS, PLATFORM_LABELS, getPlatform } from '@/lib/platforms';
import type { ApprovalAction, PendingApprovalCall } from '@/types/chat';

import { ToolApprovalJsonEditor } from './ToolApprovalJsonEditor';
import { ToolApprovalToggleGroup } from './ToolApprovalToggleGroup';

// Exact Russian copy — 17-UI-SPEC §Copywriting Contract. Inlined per
// 17-RESEARCH §Don't Hand-Roll (no shared i18n layer in v1.3).
const RU = {
  argsHeading: 'Аргументы',
  editableFieldsHint: 'Можно изменять',
  // Plan 17-08 GAP-02: discoverability hint for the inline JSON editor's
  // double-click-to-edit interaction model. Library-agnostic phrasing so
  // a future swap to a labeled-input form (per VERIFICATION §GAP-02
  // suggested fix B) does not require a copy revision.
  editAffordanceHint: 'Дважды нажмите на значение, чтобы изменить',
  rejectPlaceholder: 'Причина (необязательно)',
  rejectAriaLabel: 'Причина отказа',
  triggerExpand: 'развернуть',
  triggerCollapse: 'свернуть',
} as const;

// Bridge `@uiw/react-json-view` (root, read-only) to the shadcn neutral palette.
// Mirrors `jsonEditorTheme` from `ToolApprovalJsonEditor.tsx` so the editable
// vs. read-only swap is visually identical. Kept local per Plan 17-08 to
// avoid cross-modifying the editor file in this minimal-diff plan.
const jsonViewTheme = {
  '--w-rjv-color': 'hsl(var(--foreground))',
  '--w-rjv-background-color': 'hsl(var(--muted))',
} as React.CSSProperties;

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
  const [open, setOpen] = useState(false);

  // Auto-expand when the user picks Edit or Reject — per UI-SPEC the
  // relevant body (JSON editor or textarea) must reveal itself the moment
  // the decision is selected. Switching back to Approve does NOT force
  // close; the user may have other context they want to keep visible.
  useEffect(() => {
    if (draft.decision === 'edit' || draft.decision === 'reject') {
      setOpen(true);
    }
  }, [draft.decision]);

  const platform = getPlatform(call.toolName);
  const color = PLATFORM_COLORS[platform] ?? '#6b7280';
  const label = PLATFORM_LABELS[platform] ?? platform.toUpperCase();

  const counterOver = draft.rejectReason.length > 500;

  return (
    <div
      className={cn('rounded-md border', amberHighlighted && 'ring-2 ring-amber-400')}
      // Dynamic per-platform border color — sanctioned inline-style
      // exception shared with ToolCard.tsx / ToolCallsBlock.tsx. AGENTS.md
      // "Tailwind only" rule allows this for dynamic values.
      style={{ borderLeftColor: color, borderLeftWidth: 3 }}
    >
      <Collapsible open={open} onOpenChange={setOpen}>
        <div className="flex flex-wrap items-center gap-2 px-3 py-2">
          <CollapsibleTrigger
            aria-label={`${call.toolName} — ${open ? RU.triggerCollapse : RU.triggerExpand}`}
            className="inline-flex items-center text-gray-600 hover:text-gray-900"
          >
            {open ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
          </CollapsibleTrigger>
          <span
            className="rounded px-1.5 py-0.5 text-xs font-bold text-white"
            style={{ backgroundColor: color }}
          >
            {label}
          </span>
          <span className="font-mono text-xs text-gray-600">{call.toolName}</span>
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
            Plan 17-08 GAP-01 fix: the Аргументы block + editable-fields hint
            now render whenever the entry is expanded — gated only by
            <CollapsibleContent>, NOT by `decision`. In Edit mode the editable
            JSON view replaces the read-only one and the affordance chip
            (GAP-02) appears above it. Previously this whole block was hidden
            unless `decision === 'edit'`, blocking UI-08 (inspect-before-approve).
          */}
          <div className="space-y-2 px-3 pb-3">
            <p className="text-sm font-semibold">{RU.argsHeading}</p>
            {call.editableFields.length > 0 && (
              <p className="text-xs text-muted-foreground">
                {RU.editableFieldsHint}: {call.editableFields.join(', ')}
              </p>
            )}
            {draft.decision === 'edit' ? (
              <>
                <p
                  className="text-xs italic text-muted-foreground"
                  data-testid="edit-affordance-hint"
                >
                  {RU.editAffordanceHint}
                </p>
                <ToolApprovalJsonEditor
                  args={call.args}
                  editedArgs={draft.editedArgs}
                  editableFields={call.editableFields}
                  onEdit={onEditArg}
                />
              </>
            ) : (
              <JsonView value={call.args} collapsed={2} style={jsonViewTheme} />
            )}
          </div>

          {draft.decision === 'reject' && (
            <div className="space-y-1 px-3 pb-3">
              <Textarea
                placeholder={RU.rejectPlaceholder}
                aria-label={RU.rejectAriaLabel}
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
                {draft.rejectReason.length} / 500
              </p>
            </div>
          )}
        </CollapsibleContent>
      </Collapsible>
    </div>
  );
}
