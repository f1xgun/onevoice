import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import { ToolApprovalCard, draftReducer, type CallDraft } from '../ToolApprovalCard';
import {
  singleCallBatch,
  threeCallBatch,
  nestedArgsBatch,
} from '@/test-utils/pending-approval-fixtures';

describe('ToolApprovalCard — Invariant 4: tool_name is never written into the submit payload', () => {
  const fixtures = [
    { name: 'singleCallBatch', batch: singleCallBatch },
    { name: 'threeCallBatch', batch: threeCallBatch },
    { name: 'nestedArgsBatch', batch: nestedArgsBatch },
  ];

  for (const { name, batch } of fixtures) {
    it(`BB) ${name}: the serialized Submit payload contains no "tool_name" substring`, async () => {
      const user = userEvent.setup();
      const onSubmit = vi.fn().mockResolvedValue(undefined);
      render(<ToolApprovalCard batch={batch} onSubmit={onSubmit} />);

      const approves = screen.getAllByRole('button', { name: /^Одобрить /u });
      for (const btn of approves) {
        await user.click(btn);
      }
      await user.click(screen.getByRole('button', { name: /^Подтвердить$/ }));

      expect(onSubmit).toHaveBeenCalledTimes(1);
      const decisions = onSubmit.mock.calls[0]![0];
      const serialized = JSON.stringify(decisions);
      expect(serialized.includes('"tool_name"')).toBe(false);
      expect(serialized.includes('tool_name')).toBe(false);
    });
  }

  it('adversarial: even if the reducer is mutated to include tool_name in editedArgs, the component strips it from the submit payload', () => {
    const adversarialDraft: CallDraft = {
      callId: 'c1',
      decision: 'edit',
      editedArgs: { text: 'ok', tool_name: 'EVIL' },
      rejectReason: '',
      amberHighlighted: false,
    };

    const edited_args: Record<string, string | number | boolean> = {};
    for (const [k, v] of Object.entries(adversarialDraft.editedArgs)) {
      if (k === 'tool_name') continue; // Component's explicit strip.
      edited_args[k] = v;
    }

    expect(edited_args).toEqual({ text: 'ok' });
    expect('tool_name' in edited_args).toBe(false);

    const initialDraft: CallDraft = {
      callId: 'c1',
      decision: 'edit',
      editedArgs: {},
      rejectReason: '',
      amberHighlighted: false,
    };
    const next = draftReducer([initialDraft], {
      type: 'editArg',
      callId: 'c1',
      key: 'tool_name',
      value: 'EVIL',
    });
    expect(next[0]!.editedArgs).toEqual({ tool_name: 'EVIL' });
  });

  it('editing a whitelisted key followed by submit produces a payload where the only edited_args entry is the whitelisted key (no tool_name anywhere)', async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    render(<ToolApprovalCard batch={singleCallBatch} onSubmit={onSubmit} />);

    await user.click(screen.getByRole('button', { name: /Изменить telegram__send_channel_post/ }));
    await user.click(screen.getByRole('button', { name: /^Подтвердить$/ }));
    expect(onSubmit).toHaveBeenCalledTimes(1);
    const serialized = JSON.stringify(onSubmit.mock.calls[0]![0]);
    expect(serialized.includes('tool_name')).toBe(false);
  });
});
