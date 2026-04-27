import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import {
  ToolApprovalCard,
  draftReducer,
  type CallDraft,
  type DraftAction,
} from '../ToolApprovalCard';
import { singleCallBatch, threeCallBatch } from '@/test-utils/pending-approval-fixtures';

describe('ToolApprovalCard — card structure and gates', () => {
  it('U) renders exactly one card region per multi-call batch', () => {
    render(<ToolApprovalCard batch={threeCallBatch} onSubmit={vi.fn()} />);
    const regions = screen.queryAllByRole('region', {
      name: /Ожидает подтверждения/,
    });
    expect(regions).toHaveLength(1);
  });

  it('V) header title is "Ожидает подтверждения (N)" and subtitle matches UI-SPEC verbatim', () => {
    render(<ToolApprovalCard batch={threeCallBatch} onSubmit={vi.fn()} />);
    expect(
      screen.getByRole('heading', { level: 2, name: 'Ожидает подтверждения (3)' })
    ).toBeInTheDocument();
    expect(screen.getByText('Проверьте аргументы перед выполнением')).toBeInTheDocument();
  });

  it('W) Submit button is aria-disabled initially when any call is undecided', () => {
    render(<ToolApprovalCard batch={threeCallBatch} onSubmit={vi.fn()} />);
    const submit = screen.getByRole('button', { name: /^Подтвердить$/ });
    // aria-disabled is the semantic disabled flag (visible to SR + @testing-library).
    // The button stays clickable so Invariant 7 (amber highlight on premature
    // Submit) can fire — that behavior is tested in ToolApprovalCard.submit.test.tsx.
    expect(submit).toHaveAttribute('aria-disabled', 'true');
  });

  it('X) Submit button becomes aria-disabled="false" once every call has a decision', async () => {
    const user = userEvent.setup();
    render(<ToolApprovalCard batch={threeCallBatch} onSubmit={vi.fn()} />);
    const approveButtons = screen.getAllByRole('button', { name: /^Одобрить /u });
    for (const btn of approveButtons) {
      await user.click(btn);
    }
    const submit = screen.getByRole('button', { name: /^Подтвердить$/ });
    expect(submit).toHaveAttribute('aria-disabled', 'false');
    expect(submit).not.toBeDisabled(); // not HTML-disabled either
  });

  it('EE) a different batchId resets every draft back to undecided', async () => {
    const user = userEvent.setup();
    const { rerender } = render(<ToolApprovalCard batch={singleCallBatch} onSubmit={vi.fn()} />);
    // Approve the single call — Submit becomes aria-disabled="false".
    await user.click(screen.getByRole('button', { name: /^Одобрить / }));
    expect(screen.getByRole('button', { name: /^Подтвердить$/ })).toHaveAttribute(
      'aria-disabled',
      'false'
    );
    // Swap in a completely different batch (different batchId).
    rerender(<ToolApprovalCard batch={threeCallBatch} onSubmit={vi.fn()} />);
    // All three calls should be back to undecided → Submit aria-disabled again.
    expect(screen.getByRole('button', { name: /^Подтвердить$/ })).toHaveAttribute(
      'aria-disabled',
      'true'
    );
    // And no aria-pressed="true" should remain across the approve buttons.
    const approveButtons = screen.getAllByRole('button', { name: /^Одобрить /u });
    for (const btn of approveButtons) {
      expect(btn).toHaveAttribute('aria-pressed', 'false');
    }
  });

  it('FF) during submit, toggle groups receive disabled and the button label flips to "Отправляем…"', async () => {
    const user = userEvent.setup();
    // onSubmit returns a promise that never resolves in the window of the assertion.
    let resolvePending: () => void = () => {};
    const pendingPromise = new Promise<void>((resolve) => {
      resolvePending = resolve;
    });
    const onSubmit = vi.fn().mockReturnValue(pendingPromise);
    render(<ToolApprovalCard batch={threeCallBatch} onSubmit={onSubmit} />);

    const approveButtons = screen.getAllByRole('button', { name: /^Одобрить /u });
    for (const btn of approveButtons) {
      await user.click(btn);
    }
    await user.click(screen.getByRole('button', { name: /^Подтвердить$/ }));

    // Submit button now shows the loading label.
    expect(screen.getByRole('button', { name: /^Отправляем…$/ })).toBeInTheDocument();
    // All three "Одобрить" rows are disabled while submitting.
    for (const btn of screen.getAllByRole('button', { name: /^Одобрить /u })) {
      expect(btn).toBeDisabled();
    }

    // Clean up the pending submit so React doesn't leak warnings.
    resolvePending();
    await pendingPromise;
  });

  it('includes the Submit helper copy "Выберите действие для каждой задачи"', () => {
    render(<ToolApprovalCard batch={threeCallBatch} onSubmit={vi.fn()} />);
    expect(screen.getByText('Выберите действие для каждой задачи')).toBeInTheDocument();
  });

  // ---------- Plan 17-09 / VERIFICATION item 4: Submit hint persistence ----------
  // Regression: the visually-hidden helper span at the bottom of the footer
  // was rendered unconditionally (only the TooltipContent was gated on
  // `!allDecided`). Once a decision was picked and Submit became enabled,
  // `screen.getByText('Выберите действие для каждой задачи')` still found the
  // sr-only copy → operators saw a stale hint contradicting the enabled
  // button. Plan 17-09 gates the sr-only span on the same `!allDecided`
  // predicate.

  it('GG) keeps the Submit helper hint in the DOM while any call is undecided', () => {
    render(<ToolApprovalCard batch={threeCallBatch} onSubmit={vi.fn()} />);
    // No decisions yet → hint MUST still be visible to AT users.
    expect(screen.getByText('Выберите действие для каждой задачи')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^Подтвердить$/ })).toHaveAttribute(
      'aria-disabled',
      'true'
    );
  });

  it('HH) hides the Submit helper hint once every call has a decision', async () => {
    const user = userEvent.setup();
    render(<ToolApprovalCard batch={threeCallBatch} onSubmit={vi.fn()} />);

    // Decide every call → allDecided flips to true.
    const approveButtons = screen.getAllByRole('button', { name: /^Одобрить /u });
    for (const btn of approveButtons) {
      await user.click(btn);
    }

    const submit = screen.getByRole('button', { name: /^Подтвердить$/ });

    // 1. Confirm allDecided flipped (Submit is no longer aria-disabled).
    expect(submit).not.toHaveAttribute('aria-disabled', 'true');

    // 2. The helper copy must be ABSENT from the DOM — neither the tooltip
    //    nor the sr-only span renders. Plan 17-09 fix: gate the sr-only
    //    span on `!allDecided`.
    expect(screen.queryByText('Выберите действие для каждой задачи')).toBeNull();

    // 3. With the helper span gone, the Button's aria-describedby points
    //    nowhere. Plan 17-09 also drops the attribute when allDecided so
    //    SR output stays clean.
    expect(submit).not.toHaveAttribute('aria-describedby');
  });

  it('II) Submit helper hint is absent during in-flight resolve (allDecided + submitting)', async () => {
    const user = userEvent.setup();
    let resolvePending: () => void = () => {};
    const pendingPromise = new Promise<void>((resolve) => {
      resolvePending = resolve;
    });
    const onSubmit = vi.fn().mockReturnValue(pendingPromise);
    render(<ToolApprovalCard batch={threeCallBatch} onSubmit={onSubmit} />);

    const approveButtons = screen.getAllByRole('button', { name: /^Одобрить /u });
    for (const btn of approveButtons) {
      await user.click(btn);
    }
    await user.click(screen.getByRole('button', { name: /^Подтвердить$/ }));

    // While submitting, allDecided is true so the gated helper still does
    // NOT appear — the only visible feedback is the spinner + "Отправляем…".
    expect(screen.queryByText('Выберите действие для каждой задачи')).toBeNull();

    resolvePending();
    await pendingPromise;
  });
});

// ---------- Reducer unit tests ----------
// CC (500-char slice) + full coverage of every action.

describe('draftReducer', () => {
  const baseDrafts: CallDraft[] = [
    {
      callId: 'a',
      decision: 'undecided',
      editedArgs: {},
      rejectReason: '',
      amberHighlighted: false,
    },
    {
      callId: 'b',
      decision: 'undecided',
      editedArgs: {},
      rejectReason: '',
      amberHighlighted: false,
    },
  ];

  it('select: updates the targeted draft and clears its amber highlight', () => {
    const withAmber = baseDrafts.map((d) => ({ ...d, amberHighlighted: true }));
    const next = draftReducer(withAmber, {
      type: 'select',
      callId: 'a',
      decision: 'approve',
    });
    expect(next[0]!.decision).toBe('approve');
    expect(next[0]!.amberHighlighted).toBe(false);
    expect(next[1]!.decision).toBe('undecided'); // untouched
  });

  it('select: switching away from reject clears rejectReason', () => {
    const state: CallDraft[] = [
      {
        callId: 'a',
        decision: 'reject',
        editedArgs: {},
        rejectReason: 'typed something',
        amberHighlighted: false,
      },
    ];
    const next = draftReducer(state, { type: 'select', callId: 'a', decision: 'approve' });
    expect(next[0]!.rejectReason).toBe('');
  });

  it('select: staying on reject keeps rejectReason', () => {
    const state: CallDraft[] = [
      {
        callId: 'a',
        decision: 'reject',
        editedArgs: {},
        rejectReason: 'stay',
        amberHighlighted: false,
      },
    ];
    const next = draftReducer(state, { type: 'select', callId: 'a', decision: 'reject' });
    expect(next[0]!.rejectReason).toBe('stay');
  });

  it('editArg: writes exactly one top-level key to editedArgs', () => {
    const next = draftReducer(baseDrafts, {
      type: 'editArg',
      callId: 'a',
      key: 'text',
      value: 'hello',
    });
    expect(next[0]!.editedArgs).toEqual({ text: 'hello' });
    expect(next[1]!.editedArgs).toEqual({});
  });

  it('CC) setRejectReason slices strings longer than 500 chars down to exactly 500', () => {
    const input = 'x'.repeat(600);
    const next = draftReducer(baseDrafts, {
      type: 'setRejectReason',
      callId: 'a',
      reason: input,
    });
    expect(next[0]!.rejectReason.length).toBe(500);
    // Sanity check — every character is the same 'x' we put in.
    expect(/^x+$/.test(next[0]!.rejectReason)).toBe(true);
  });

  it('setRejectReason: a 500-char string passes through unchanged', () => {
    const input = 'y'.repeat(500);
    const next = draftReducer(baseDrafts, {
      type: 'setRejectReason',
      callId: 'a',
      reason: input,
    });
    expect(next[0]!.rejectReason.length).toBe(500);
  });

  it('highlightUndecided: only the listed callIds with undecided decision get amberHighlighted=true', () => {
    const state: CallDraft[] = [{ ...baseDrafts[0]! }, { ...baseDrafts[1]!, decision: 'approve' }];
    const next = draftReducer(state, {
      type: 'highlightUndecided',
      callIds: ['a', 'b'],
    });
    expect(next[0]!.amberHighlighted).toBe(true); // undecided + listed
    expect(next[1]!.amberHighlighted).toBe(false); // listed but decided
  });

  it('clearHighlights: wipes amberHighlighted on every row', () => {
    const state = baseDrafts.map((d) => ({ ...d, amberHighlighted: true }));
    const next = draftReducer(state, { type: 'clearHighlights' });
    for (const d of next) {
      expect(d.amberHighlighted).toBe(false);
    }
  });

  it('reset: replaces the state with the provided drafts array', () => {
    const replacement: CallDraft[] = [
      {
        callId: 'z',
        decision: 'approve',
        editedArgs: { text: 'x' },
        rejectReason: '',
        amberHighlighted: false,
      },
    ];
    const next = draftReducer(baseDrafts, { type: 'reset', drafts: replacement });
    expect(next).toBe(replacement);
  });

  it('exhaustive DraftAction coverage: every action shape compiles and is handled', () => {
    const actions: DraftAction[] = [
      { type: 'select', callId: 'a', decision: 'approve' },
      { type: 'editArg', callId: 'a', key: 'k', value: 1 },
      { type: 'setRejectReason', callId: 'a', reason: '' },
      { type: 'highlightUndecided', callIds: [] },
      { type: 'clearHighlights' },
      { type: 'reset', drafts: [] },
    ];
    for (const a of actions) {
      const next = draftReducer(baseDrafts, a);
      expect(Array.isArray(next)).toBe(true);
    }
  });
});
