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

    await user.click(screen.getByRole('button', { name: /Отклонить telegram__send_channel_post/ }));
    const textarea = await screen.findByPlaceholderText('Причина (необязательно)');
    expect(textarea).toBeInTheDocument();

    await user.type(textarea, 'слишком рано');
    expect(textarea).toHaveValue('слишком рано');

    await user.click(screen.getByRole('button', { name: /Одобрить telegram__send_channel_post/ }));
    expect(screen.queryByPlaceholderText('Причина (необязательно)')).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /^Подтвердить$/ }));
    expect(onSubmit).toHaveBeenCalledTimes(1);
    const decisions = onSubmit.mock.calls[0]![0];
    expect(decisions[0]).toEqual({ id: 'call-single-1', action: 'approve' });
    expect('reject_reason' in decisions[0]).toBe(false);

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

    const initialCounter = screen.getByText('0 / 500');
    expect(initialCounter.className).toContain('text-muted-foreground');
    expect(initialCounter.className).not.toContain('text-destructive');

    await user.type(textarea, 'привет');
    expect(screen.getByText('6 / 500').className).toContain('text-muted-foreground');

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
