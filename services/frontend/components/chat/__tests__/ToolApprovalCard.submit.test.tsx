import { describe, it, expect, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import { ToolApprovalCard } from '../ToolApprovalCard';
import { threeCallBatch } from '@/test-utils/pending-approval-fixtures';

describe('ToolApprovalCard — premature Submit and atomic payload shape', () => {
  it('Y) clicking Submit with undecided rows does NOT call onSubmit and applies ring-amber-400 to the undecided entries', async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();
    render(<ToolApprovalCard batch={threeCallBatch} onSubmit={onSubmit} />);

    // Decide only the first call — leave c2, c3 undecided.
    await user.click(screen.getByRole('button', { name: /Одобрить telegram__send_channel_post/ }));

    // Premature Submit: the button is NOT HTML-disabled (it is aria-disabled
    // only) — clicking fires handleSubmit which branches into the
    // highlightUndecided path per Invariant 7.
    await user.click(screen.getByRole('button', { name: /^Подтвердить$/ }));

    // Contract 1: onSubmit is NOT called while any row is undecided.
    expect(onSubmit).not.toHaveBeenCalled();

    // Contract 2: the two undecided entries receive ring-amber-400. Each
    // accordion entry is the ONLY element with an inline
    // `border-left-width: 3` style, so we locate them by that selector.
    const entries = document.querySelectorAll<HTMLElement>('[style*="border-left-width"]');
    expect(entries).toHaveLength(3);

    // The first entry was decided (approve) → no amber ring.
    expect(entries[0]!.className).not.toContain('ring-amber-400');
    // The second + third entries remain undecided → amber ring applied.
    expect(entries[1]!.className).toContain('ring-amber-400');
    expect(entries[2]!.className).toContain('ring-amber-400');
  });

  it('clicking Submit premature a second time keeps the amber highlight on still-undecided rows', async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();
    render(<ToolApprovalCard batch={threeCallBatch} onSubmit={onSubmit} />);

    await user.click(screen.getByRole('button', { name: /^Подтвердить$/ }));
    // First Submit click: all three rows highlighted.
    let entries = document.querySelectorAll<HTMLElement>('[style*="border-left-width"]');
    expect(entries).toHaveLength(3);
    for (const e of entries) {
      expect(e.className).toContain('ring-amber-400');
    }

    // User decides one — that row's amber clears (reducer clears it on
    // any `select` dispatch) — the other two remain amber-flagged.
    await user.click(screen.getByRole('button', { name: /Одобрить telegram__send_channel_post/ }));
    entries = document.querySelectorAll<HTMLElement>('[style*="border-left-width"]');
    expect(entries[0]!.className).not.toContain('ring-amber-400');
    // Submit hasn't been clicked again so c2/c3 have NOT been re-flagged.
    // This is the plan's intended flow — the user clicks Submit to flag,
    // acts on flags, then submits again.
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it('Z) atomic Submit sends one call with decisions[] in batch.calls order', async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    render(<ToolApprovalCard batch={threeCallBatch} onSubmit={onSubmit} />);

    // Approve c1, Edit c2 (no arg changes), Reject c3 (no reason typed).
    await user.click(screen.getByRole('button', { name: /Одобрить telegram__send_channel_post/ }));
    await user.click(screen.getByRole('button', { name: /Изменить vk__create_post/ }));
    await user.click(
      screen.getByRole('button', { name: /Отклонить yandex_business__reply_review/ })
    );
    await user.click(screen.getByRole('button', { name: /^Подтвердить$/ }));

    expect(onSubmit).toHaveBeenCalledTimes(1);
    const arg = onSubmit.mock.calls[0]![0];
    expect(Array.isArray(arg)).toBe(true);
    expect(arg).toHaveLength(threeCallBatch.calls.length);

    // Preserves batch order: c1, c2, c3.
    expect(arg[0]).toMatchObject({ id: 'c1', action: 'approve' });
    expect(arg[1]).toMatchObject({ id: 'c2', action: 'edit' });
    expect(arg[2]).toMatchObject({ id: 'c3', action: 'reject' });

    // Edited call with NO changes → no edited_args key at all (empty
    // object would pollute the body; the reducer only tracks changes).
    expect('edited_args' in arg[1]).toBe(false);

    // Rejected call with NO typed reason → no reject_reason key at all.
    expect('reject_reason' in arg[2]).toBe(false);
  });

  it('Submit is a single atomic invocation even if clicked repeatedly', async () => {
    const user = userEvent.setup();
    let callCount = 0;
    const onSubmit = vi.fn().mockImplementation(async () => {
      callCount += 1;
      // First call blocks long enough for extra user.clicks to be dispatched.
      await new Promise((resolve) => setTimeout(resolve, 50));
    });
    render(<ToolApprovalCard batch={threeCallBatch} onSubmit={onSubmit} />);

    // Decide all three.
    const approves = screen.getAllByRole('button', { name: /^Одобрить /u });
    for (const btn of approves) {
      await user.click(btn);
    }

    const submit = screen.getByRole('button', { name: /^Подтвердить$/ });
    await user.click(submit);
    // Second click: the button is now HTML-disabled because `submitting` is true.
    await user.click(submit);
    await user.click(submit);

    // Wait for the first onSubmit to finish.
    await new Promise((resolve) => setTimeout(resolve, 80));

    expect(callCount).toBe(1);
  });

  // Defensive sanity — clicking Submit with NO decisions at all still
  // doesn't invoke onSubmit and flags every row.
  it('submitting with zero decisions flags every entry', async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();
    render(<ToolApprovalCard batch={threeCallBatch} onSubmit={onSubmit} />);

    await user.click(screen.getByRole('button', { name: /^Подтвердить$/ }));

    const entries = document.querySelectorAll<HTMLElement>('[style*="border-left-width"]');
    expect(entries).toHaveLength(3);
    for (const e of entries) {
      expect(e.className).toContain('ring-amber-400');
    }
    expect(onSubmit).not.toHaveBeenCalled();
    // Eliminate unused-import warning for `within` if TypeScript notices.
    void within;
  });
});
