import { describe, it, expect, vi } from 'vitest';
import { render, screen, within, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import { ToolApprovalCard } from '../ToolApprovalCard';
import { threeCallBatch } from '@/test-utils/pending-approval-fixtures';

describe('ToolApprovalCard — premature Submit and atomic payload shape', () => {
  it('Y) clicking Submit with undecided rows does NOT call onSubmit and applies ring-warning to the undecided entries', async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();
    render(<ToolApprovalCard batch={threeCallBatch} onSubmit={onSubmit} />);

    await user.click(screen.getByRole('button', { name: /Одобрить telegram__send_channel_post/ }));

    await user.click(screen.getByRole('button', { name: /^Подтвердить$/ }));

    expect(onSubmit).not.toHaveBeenCalled();

    const entries = document.querySelectorAll<HTMLElement>('[data-approval-call]');
    expect(entries).toHaveLength(3);

    expect(entries[0]!.className).not.toContain('ring-warning');
    expect(entries[1]!.className).toContain('ring-warning');
    expect(entries[2]!.className).toContain('ring-warning');
  });

  it('clicking Submit premature a second time keeps the amber highlight on still-undecided rows', async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();
    render(<ToolApprovalCard batch={threeCallBatch} onSubmit={onSubmit} />);

    await user.click(screen.getByRole('button', { name: /^Подтвердить$/ }));
    let entries = document.querySelectorAll<HTMLElement>('[data-approval-call]');
    expect(entries).toHaveLength(3);
    for (const e of entries) {
      expect(e.className).toContain('ring-warning');
    }

    await user.click(screen.getByRole('button', { name: /Одобрить telegram__send_channel_post/ }));
    entries = document.querySelectorAll<HTMLElement>('[data-approval-call]');
    expect(entries[0]!.className).not.toContain('ring-warning');
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it('Z) atomic Submit sends one call with decisions[] in batch.calls order', async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    render(<ToolApprovalCard batch={threeCallBatch} onSubmit={onSubmit} />);

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

    expect(arg[0]).toMatchObject({ id: 'c1', action: 'approve' });
    expect(arg[1]).toMatchObject({ id: 'c2', action: 'edit' });
    expect(arg[2]).toMatchObject({ id: 'c3', action: 'reject' });

    expect('edited_args' in arg[1]).toBe(false);

    expect('reject_reason' in arg[2]).toBe(false);
  });

  it('Submit is a single atomic invocation even if clicked repeatedly', async () => {
    const user = userEvent.setup();
    let callCount = 0;
    const onSubmit = vi.fn().mockImplementation(async () => {
      callCount += 1;
      await new Promise((resolve) => setTimeout(resolve, 50));
    });
    render(<ToolApprovalCard batch={threeCallBatch} onSubmit={onSubmit} />);

    const approves = screen.getAllByRole('button', { name: /^Одобрить /u });
    for (const btn of approves) {
      await user.click(btn);
    }

    const submit = screen.getByRole('button', { name: /^Подтвердить$/ });

    fireEvent.click(submit);
    fireEvent.click(submit);
    fireEvent.click(submit);

    await new Promise((resolve) => setTimeout(resolve, 200));

    expect(callCount).toBe(1);
  });

  it('submitting with zero decisions flags every entry', async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();
    render(<ToolApprovalCard batch={threeCallBatch} onSubmit={onSubmit} />);

    await user.click(screen.getByRole('button', { name: /^Подтвердить$/ }));

    const entries = document.querySelectorAll<HTMLElement>('[data-approval-call]');
    expect(entries).toHaveLength(3);
    for (const e of entries) {
      expect(e.className).toContain('ring-warning');
    }
    expect(onSubmit).not.toHaveBeenCalled();
    void within;
  });
});
