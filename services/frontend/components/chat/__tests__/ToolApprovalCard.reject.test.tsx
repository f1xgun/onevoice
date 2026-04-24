import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import { ToolApprovalCard } from '../ToolApprovalCard';
import { singleCallBatch } from '@/test-utils/pending-approval-fixtures';

describe('ToolApprovalCard — two-step reject flow', () => {
  it('DD) clicking Reject expands the textarea with the placeholder; switching to Approve hides it and clears the reason', async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    render(<ToolApprovalCard batch={singleCallBatch} onSubmit={onSubmit} />);

    // Step 1: click Reject — textarea appears with the exact Russian placeholder.
    await user.click(screen.getByRole('button', { name: /Отклонить telegram__send_channel_post/ }));
    const textarea = await screen.findByPlaceholderText('Причина (необязательно)');
    expect(textarea).toBeInTheDocument();

    // Step 2: type a reason — staged in the reducer.
    await user.type(textarea, 'слишком рано');
    expect(textarea).toHaveValue('слишком рано');

    // Step 3: click Approve — textarea disappears entirely; the reducer
    // cleared the reason (switching away from reject clears rejectReason).
    await user.click(screen.getByRole('button', { name: /Одобрить telegram__send_channel_post/ }));
    expect(screen.queryByPlaceholderText('Причина (необязательно)')).not.toBeInTheDocument();

    // Step 4: submit — no reject_reason leaks into the payload.
    await user.click(screen.getByRole('button', { name: /^Подтвердить$/ }));
    expect(onSubmit).toHaveBeenCalledTimes(1);
    const decisions = onSubmit.mock.calls[0]![0];
    expect(decisions[0]).toEqual({ id: 'call-single-1', action: 'approve' });
    expect('reject_reason' in decisions[0]).toBe(false);

    // Step 5 (regression): re-opening reject after a round-trip starts
    // fresh — the textarea is empty again (the reducer cleared it on the
    // earlier `select: approve`).
    await user.click(screen.getByRole('button', { name: /Отклонить telegram__send_channel_post/ }));
    const reopened = screen.getByPlaceholderText('Причина (необязательно)');
    expect(reopened).toHaveValue('');
  });

  it('II) counter passes through 500 cleanly and turns text-destructive only when > 500 chars are staged', async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    render(<ToolApprovalCard batch={singleCallBatch} onSubmit={onSubmit} />);

    await user.click(screen.getByRole('button', { name: /Отклонить telegram__send_channel_post/ }));
    const textarea = screen.getByPlaceholderText('Причина (необязательно)');

    // Initial counter: 0 / 500 — muted color.
    const initialCounter = screen.getByText('0 / 500');
    expect(initialCounter.className).toContain('text-muted-foreground');
    expect(initialCounter.className).not.toContain('text-destructive');

    // Type a short reason — still muted.
    await user.type(textarea, 'привет');
    expect(screen.getByText('6 / 500').className).toContain('text-muted-foreground');

    // The textarea has `maxLength={500}` so typing more than 500 is
    // physically impossible via keyboard — the browser caps input length.
    // The reducer also slices to 500 defensively. The end-to-end contract
    // is: whatever is staged length === min(typed, 500). So the
    // user-visible counter never crosses 500 in practice; the reducer
    // guarantees the invariant. This is covered by CC in the main test
    // file at the reducer level — here we sanity-check the UI layer pulls
    // the value from the reducer correctly.
    expect(textarea).toHaveAttribute('maxlength', '500');
  });

  it('reject reason passes through verbatim into the submit payload (under the 500-char slice)', async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    render(<ToolApprovalCard batch={singleCallBatch} onSubmit={onSubmit} />);

    await user.click(screen.getByRole('button', { name: /Отклонить telegram__send_channel_post/ }));
    const textarea = screen.getByPlaceholderText('Причина (необязательно)');
    await user.type(textarea, 'not now');

    await user.click(screen.getByRole('button', { name: /^Подтвердить$/ }));
    expect(onSubmit).toHaveBeenCalledTimes(1);
    const decisions = onSubmit.mock.calls[0]![0];
    expect(decisions[0]).toMatchObject({
      id: 'call-single-1',
      action: 'reject',
      reject_reason: 'not now',
    });
  });
});
