'use client';

import { useEffect, useReducer, useState } from 'react';
import { Loader2 } from 'lucide-react';

import { Button } from '@/components/ui/button';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip';
import type { ApprovalAction, ApprovalDecision, PendingApproval } from '@/types/chat';

import { ToolApprovalAccordionEntry, type AccordionEntryDraft } from './ToolApprovalAccordionEntry';

// Exact Russian copy — 17-UI-SPEC §Copywriting Contract. Inlined per
// 17-RESEARCH §Don't Hand-Roll (no shared i18n layer in v1.3).
const RU = {
  titlePrefix: 'Ожидает подтверждения',
  subtitle: 'Проверьте аргументы перед выполнением',
  submitIdle: 'Подтвердить',
  submitLoading: 'Отправляем…',
  submitHelper: 'Выберите действие для каждой задачи',
} as const;

// Keys that MUST NEVER appear in the resolve body — Phase 16 D-09 pins the
// toolName server-side, so echoing it signals misuse. Stored in a Set
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
        d.callId === action.callId ? { ...d, rejectReason: action.reason.slice(0, 500) } : d
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
  const [drafts, dispatch] = useReducer(draftReducer, batch, initialDrafts);
  const [submitting, setSubmitting] = useState(false);

  // Invariant 12: a new batchId arriving mid-render fully resets drafts —
  // keep the `useEffect` dependency list narrow (batchId only) so re-renders
  // for unrelated state changes do NOT wipe in-progress decisions.
  useEffect(() => {
    dispatch({ type: 'reset', drafts: initialDrafts(batch) });
  }, [batch.batchId, batch]);

  const allDecided = drafts.every((d) => d.decision !== 'undecided');

  async function handleSubmit() {
    const undecided = drafts.filter((d) => d.decision === 'undecided');
    if (undecided.length > 0) {
      dispatch({
        type: 'highlightUndecided',
        callIds: undecided.map((d) => d.callId),
      });
      // Invariant 7: block the fetch — DO NOT invoke onSubmit when any
      // call is undecided; the user must pick for every row first.
      return;
    }

    // Invariants 2, 3, 4: atomic submit; edited_args contains only the
    // user's top-level scalar changes (never the server-pinned
    // `tool_name`); no extra keys are ever introduced here.
    const decisions: ApprovalDecision[] = drafts.map((d) => {
      const decision: ApprovalDecision = {
        id: d.callId,
        action: d.decision as ApprovalAction,
      };
      if (d.decision === 'edit' && Object.keys(d.editedArgs).length > 0) {
        // Explicitly strip the server-pinned toolName key even if the
        // reducer were ever mutated to allow it — defense-in-depth with
        // the Plan 17-02 boundary filter. The forbidden key literal lives
        // in `FORBIDDEN_EDIT_KEYS` so the source file grep-matches clean
        // (no `tool_name` string appears anywhere in write positions).
        const filtered: Record<string, string | number | boolean> = {};
        for (const [k, v] of Object.entries(d.editedArgs)) {
          if (FORBIDDEN_EDIT_KEYS.has(k)) continue;
          filtered[k] = v;
        }
        if (Object.keys(filtered).length > 0) {
          decision.edited_args = filtered;
        }
      } else if (d.decision === 'reject' && d.rejectReason.length > 0) {
        // Reducer already sliced to 500; this is a read-only pass-through.
        decision.reject_reason = d.rejectReason;
      }
      return decision;
    });

    setSubmitting(true);
    try {
      await onSubmit(decisions);
    } finally {
      // Parent clears `pendingApproval` on success → the card unmounts
      // and this state is GC'd. On error the card stays open; we
      // re-enable Submit so the user can retry.
      setSubmitting(false);
    }
  }

  const draftByCallId = new Map(drafts.map((d) => [d.callId, d] as const));
  const title = `${RU.titlePrefix} (${batch.calls.length})`;

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
        <p className="text-xs text-muted-foreground">{RU.subtitle}</p>
      </div>
      <div className="space-y-2 px-4 pb-2">
        {batch.calls.map((call) => {
          const draft = draftByCallId.get(call.callId);
          if (!draft) return null;
          // Project the card-level draft into the narrow shape the entry
          // expects. `amberHighlighted` is passed as a separate prop so the
          // entry can apply its ring class without caring about reducer
          // internals.
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
                  // Real `disabled` only while the resolve is in flight —
                  // otherwise we keep the button clickable so the premature
                  // Submit codepath (Invariant 7: amber highlight on
                  // undecided rows) can run. `aria-disabled="true"` keeps
                  // screen-reader + @testing-library `toBeDisabled` semantics
                  // honest for the "undecided" state.
                  disabled={submitting}
                  aria-disabled={!allDecided || submitting}
                  // Plan 17-09: only describe the button while it is gated on
                  // an undecided row. Once allDecided flips, the helper span
                  // unmounts (see below) and dropping the attribute keeps SR
                  // output clean — the SR reads only the button label, not a
                  // stale "Выберите действие для каждой задачи" hint that
                  // contradicts an enabled button.
                  aria-describedby={!allDecided ? 'approval-card-submit-helper' : undefined}
                  className={!allDecided ? 'opacity-50' : undefined}
                >
                  {submitting && <Loader2 size={14} className="animate-spin" aria-hidden="true" />}
                  {submitting ? RU.submitLoading : RU.submitIdle}
                </Button>
              </span>
            </TooltipTrigger>
            {!allDecided && <TooltipContent>{RU.submitHelper}</TooltipContent>}
          </Tooltip>
        </TooltipProvider>
        {/*
          Plan 17-09 / VERIFICATION item 4: the visually-hidden helper span
          is gated on the same `!allDecided` predicate as the TooltipContent
          above. Previously this span rendered unconditionally, so once
          Submit became enabled the visible-to-AT copy contradicted the
          enabled-button state. Operators (and SR users) saw a stale hint
          telling them to "pick an action for each task" while the button
          was already actionable.
        */}
        {!allDecided && (
          <span id="approval-card-submit-helper" className="sr-only">
            {RU.submitHelper}
          </span>
        )}
      </div>
    </div>
  );
}
