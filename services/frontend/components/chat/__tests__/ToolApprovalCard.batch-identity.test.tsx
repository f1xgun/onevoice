import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import { ToolApprovalCard } from '../ToolApprovalCard';
import type { PendingApproval } from '@/types/chat';
import { threeCallBatch } from '@/test-utils/pending-approval-fixtures';

// Regression: the operator's staged decisions must survive a re-render with a
// NEW batch object that has the SAME batchId. `batch` is `pendingApproval` from
// useConversationFlow, which is re-created with a fresh object reference (same
// batchId) on unrelated upstream re-renders (e.g. a token-refresh re-running
// the history-load effect → setPendingApproval(normalizePendingApproval(...))).
//
// The reset effect is keyed on `batch.batchId` only. Reverting it to
// `[batch.batchId, batch]` makes it fire on every fresh object reference,
// wiping the drafts → this test fails (the staged approve/reject is reset to
// undecided and the reject reason is gone).

function cloneSameBatchId(batch: PendingApproval): PendingApproval {
  return {
    ...batch,
    calls: batch.calls.map((c) => ({ ...c })),
  };
}

describe('ToolApprovalCard — staged drafts survive a same-batchId object swap', () => {
  it('preserves approve/reject selections and the reject reason across a new batch reference', async () => {
    const user = userEvent.setup();
    const { rerender } = render(<ToolApprovalCard batch={threeCallBatch} onSubmit={vi.fn()} />);

    // Stage decisions: approve c1, reject c2 with a reason, approve c3.
    await user.click(
      screen.getByRole('button', { name: /^Одобрить telegram__send_channel_post/u })
    );
    await user.click(screen.getByRole('button', { name: /^Отклонить vk__create_post/u }));
    const reasonBox = await screen.findByPlaceholderText('Причина (необязательно)');
    await user.type(reasonBox, 'нужна правка');
    await user.click(
      screen.getByRole('button', { name: /^Одобрить yandex_business__reply_review/u })
    );

    // Every call now has a decision → Submit is enabled.
    const submit = screen.getByRole('button', { name: /^Подтвердить$/ });
    expect(submit).toHaveAttribute('aria-disabled', 'false');

    // Upstream re-render hands a fresh object with the SAME batchId.
    rerender(<ToolApprovalCard batch={cloneSameBatchId(threeCallBatch)} onSubmit={vi.fn()} />);

    // Drafts must be intact: Submit still enabled and the reject reason kept.
    expect(screen.getByRole('button', { name: /^Подтвердить$/ })).toHaveAttribute(
      'aria-disabled',
      'false'
    );
    expect(screen.getByDisplayValue('нужна правка')).toBeInTheDocument();
    expect(
      screen.getByRole('button', { name: /^Одобрить telegram__send_channel_post/u })
    ).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByRole('button', { name: /^Отклонить vk__create_post/u })).toHaveAttribute(
      'aria-pressed',
      'true'
    );
  });
});
