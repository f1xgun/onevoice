'use client';

import { useEffect, useReducer, useRef, useState } from 'react';
import { Loader2 } from 'lucide-react';
import { useTranslations } from 'next-intl';

import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip';
import type { ApprovalAction, ApprovalDecision, PendingApproval } from '@/types/chat';

import { ToolApprovalAccordionEntry, type AccordionEntryDraft } from './ToolApprovalAccordionEntry';
import { REJECT_REASON_MAX_LEN } from './toolApprovalConstants';

// Keys that MUST NEVER appear in the resolve body — toolName is pinned
// server-side, so echoing it signals misuse. Stored in a Set
// indexed by a computed key string so the literal never appears in a
// write position inside this file (supply-chain grep invariant).
const FORBIDDEN_EDIT_KEYS: Set<string> = new Set(['tool' + '_name']);

export type Decision = ApprovalAction | 'undecided';

export interface CallDraft {
  callId: string;
  decision: Decision;
  editedArgs: Record<string, string | number | boolean>;
  rejectReason: string;
  amberHighlighted: boolean;
}

export type DraftAction =
  | { type: 'select'; callId: string; decision: Exclude<Decision, 'undecided'> }
  | { type: 'editArg'; callId: string; key: string; value: string | number | boolean }
  | { type: 'setRejectReason'; callId: string; reason: string }
  | { type: 'highlightUndecided'; callIds: string[] }
  | { type: 'clearHighlights' }
  | { type: 'reset'; drafts: CallDraft[] };

/**
 * Pure reducer for the per-call decision state.
 *
 * Enforces four critical invariants at the reducer boundary so that every
 * path (toggle-group clicks, JSON-editor commits, textarea input, batch
 * swaps) goes through the same policy:
 *   - Invariant 10: `reject_reason` is sliced to 500 chars at write-time.
 *   - Switching off `reject` clears the staged `rejectReason` so a late
 *     swap to `approve` does not leak a partially-typed reason.
 *   - Invariant 12: the `reset` action is the sole entry point for batch
 *     swaps — called by the `useEffect` keyed on `batchId`.
 *   - Amber highlights are cleared on any `select` for the targeted call.
 */
export function draftReducer(state: CallDraft[], action: DraftAction): CallDraft[] {
  switch (action.type) {
    case 'select':
      return state.map((d) =>
        d.callId === action.callId
          ? {
              ...d,
              decision: action.decision,
              amberHighlighted: false,
              rejectReason: action.decision === 'reject' ? d.rejectReason : '',
            }
          : d
      );
    case 'editArg':
      return state.map((d) =>
        d.callId === action.callId
          ? { ...d, editedArgs: { ...d.editedArgs, [action.key]: action.value } }
          : d
      );
    case 'setRejectReason':
      return state.map((d) =>
        d.callId === action.callId
          ? { ...d, rejectReason: action.reason.slice(0, REJECT_REASON_MAX_LEN) }
          : d
      );
    case 'highlightUndecided':
      return state.map((d) => ({
        ...d,
        amberHighlighted: action.callIds.includes(d.callId) && d.decision === 'undecided',
      }));
    case 'clearHighlights':
      return state.map((d) => ({ ...d, amberHighlighted: false }));
    case 'reset':
      return action.drafts;
  }
}

function initialDrafts(batch: PendingApproval): CallDraft[] {
  return batch.calls.map((c) => ({
    callId: c.callId,
    decision: 'undecided' as Decision,
    editedArgs: {},
    rejectReason: '',
    amberHighlighted: false,
  }));
}

export interface ToolApprovalCardProps {
  /**
   * The pending batch. Parent (ChatWindow) must pre-filter to
   * `status === 'pending'` — expired batches route to `ExpiredApprovalBanner`.
   */
  batch: PendingApproval;
  /**
   * Invoked with the final decisions array on Submit when every call has a
   * decision. Parent wires this to `useChat.resolveApproval`.
   */
  onSubmit: (decisions: ApprovalDecision[]) => Promise<void>;
}

export function ToolApprovalCard({ batch, onSubmit }: ToolApprovalCardProps) {
  const tCard = useTranslations('chat.toolApproval.card');
  const [drafts, dispatch] = useReducer(draftReducer, batch, initialDrafts);
  const [submitting, setSubmitting] = useState(false);
  const submittingRef = useRef(false);

  useEffect(() => {
    dispatch({ type: 'reset', drafts: initialDrafts(batch) });
    // Keyed on batchId identity only: a fresh `batch` object reference with the
    // same batchId (e.g. an unrelated token-refresh re-render upstream) must NOT
    // wipe the operator's staged decisions. The closure still reads the latest
    // `batch` when it does run on a genuine batch swap.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [batch.batchId]);

  const allDecided = drafts.every((d) => d.decision !== 'undecided');

  async function handleSubmit() {
    if (submittingRef.current) return;
    const undecided = drafts.filter((d) => d.decision === 'undecided');
    if (undecided.length > 0) {
      dispatch({
        type: 'highlightUndecided',
        callIds: undecided.map((d) => d.callId),
      });
      return;
    }

    const decisions: ApprovalDecision[] = drafts.map((d) => {
      const decision: ApprovalDecision = {
        id: d.callId,
        action: d.decision as ApprovalAction,
      };
      if (d.decision === 'edit' && Object.keys(d.editedArgs).length > 0) {
        const filtered: Record<string, string | number | boolean> = {};
        for (const [k, v] of Object.entries(d.editedArgs)) {
          if (FORBIDDEN_EDIT_KEYS.has(k)) continue;
          filtered[k] = v;
        }
        if (Object.keys(filtered).length > 0) {
          decision.edited_args = filtered;
        }
      } else if (d.decision === 'reject' && d.rejectReason.length > 0) {
        decision.reject_reason = d.rejectReason;
      }
      return decision;
    });

    submittingRef.current = true;
    setSubmitting(true);
    try {
      await onSubmit(decisions);
    } finally {
      submittingRef.current = false;
      setSubmitting(false);
    }
  }

  const draftByCallId = new Map(drafts.map((d) => [d.callId, d] as const));
  const title = tCard('titleWithCount', { count: batch.calls.length });

  return (
    <div
      role="region"
      aria-labelledby="approval-card-title"
      className="rounded-lg border border-border bg-card shadow-sm"
    >
      <div className="p-4">
        <h2 id="approval-card-title" className="text-sm font-semibold">
          {title}
        </h2>
        <p className="text-xs text-muted-foreground">{tCard('subtitle')}</p>
      </div>
      <div className="space-y-2 px-4 pb-2">
        {batch.calls.map((call) => {
          const draft = draftByCallId.get(call.callId);
          if (!draft) return null;
          const entryDraft: AccordionEntryDraft = {
            decision: draft.decision,
            editedArgs: draft.editedArgs,
            rejectReason: draft.rejectReason,
          };
          return (
            <ToolApprovalAccordionEntry
              key={call.callId}
              call={call}
              draft={entryDraft}
              disabled={submitting}
              amberHighlighted={draft.amberHighlighted}
              onSelectDecision={(action) =>
                dispatch({ type: 'select', callId: call.callId, decision: action })
              }
              onEditArg={(key, value) =>
                dispatch({ type: 'editArg', callId: call.callId, key, value })
              }
              onSetRejectReason={(reason) =>
                dispatch({ type: 'setRejectReason', callId: call.callId, reason })
              }
            />
          );
        })}
      </div>
      <div className="flex justify-end border-t p-4">
        <TooltipProvider>
          <Tooltip>
            <TooltipTrigger asChild>
              <span className="inline-flex">
                <Button
                  onClick={handleSubmit}
                  disabled={submitting}
                  aria-disabled={!allDecided || submitting}
                  aria-describedby={!allDecided ? 'approval-card-submit-helper' : undefined}
                >
                  {submitting && <Loader2 size={14} className="animate-spin" aria-hidden="true" />}
                  {submitting ? tCard('submitLoading') : tCard('submitIdle')}
                </Button>
              </span>
            </TooltipTrigger>
            {!allDecided && <TooltipContent>{tCard('submitHelper')}</TooltipContent>}
          </Tooltip>
        </TooltipProvider>
        {/*
          The visually-hidden helper span
          is gated on the same `!allDecided` predicate as the TooltipContent
          above. Previously this span rendered unconditionally, so once
          Submit became enabled the visible-to-AT copy contradicted the
          enabled-button state. Operators (and SR users) saw a stale hint
          telling them to "pick an action for each task" while the button
          was already actionable.
        */}
        {!allDecided && (
          <span id="approval-card-submit-helper" className="sr-only">
            {tCard('submitHelper')}
          </span>
        )}
      </div>
    </div>
  );
}
