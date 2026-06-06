import { describe, it, expect, vi } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import { ToolApprovalCard, draftReducer, type CallDraft } from '../ToolApprovalCard';
import { evaluateEditGate } from '../toolApprovalGate';
import { singleCallBatch, nestedArgsBatch } from '@/test-utils/pending-approval-fixtures';

// Helper: apply an edit through the same pipeline the form uses at runtime:
// evaluateEditGate → (accepted) → reducer.editArg. This mirrors the
// component's onEdit closure without simulating the full DOM event chain.
function applyEdit(
  drafts: CallDraft[],
  callId: string,
  key: string,
  value: string | number | boolean,
  editableFields: string[],
  parentName?: string | number
): CallDraft[] {
  const accepted = evaluateEditGate(
    {
      value,
      oldValue: null,
      keyName: key,
      parentName,
      type: 'value',
    },
    editableFields
  );
  if (!accepted) return drafts;
  return draftReducer(drafts, { type: 'editArg', callId, key, value });
}

describe('ToolApprovalCard.edited_args — Invariant 3: only top-level scalar changes are submitted', () => {
  it('AA) scalar edit to a whitelisted top-level key lands in edited_args; nested meta.text is rejected by the gate and never reaches the payload', async () => {
    const batch = nestedArgsBatch;
    let drafts: CallDraft[] = batch.calls.map((c) => ({
      callId: c.callId,
      decision: 'edit',
      editedArgs: {},
      rejectReason: '',
      amberHighlighted: false,
    }));
    const call = batch.calls[0]!; // tool_with_nested_args, editableFields: ['text']

    drafts = applyEdit(drafts, call.callId, 'text', 'new-top', call.editableFields);
    expect(drafts[0]!.editedArgs).toEqual({ text: 'new-top' });

    drafts = applyEdit(drafts, call.callId, 'text', 'nested-new', call.editableFields, 'meta');
    expect(drafts[0]!.editedArgs).toEqual({ text: 'new-top' }); // unchanged
    expect('meta' in drafts[0]!.editedArgs).toBe(false);

    drafts = applyEdit(drafts, call.callId, 'author', 'bob', call.editableFields);
    expect('author' in drafts[0]!.editedArgs).toBe(false);

    const entry = drafts[0]!;
    const edited_args: Record<string, string | number | boolean> = {};
    for (const [k, v] of Object.entries(entry.editedArgs)) {
      if (k === 'tool_name') continue;
      edited_args[k] = v;
    }
    expect(edited_args).toEqual({ text: 'new-top' });
    expect(JSON.stringify(edited_args)).not.toContain('meta');
    expect(JSON.stringify(edited_args)).not.toContain('author');
  });

  it('mounting ToolApprovalCard with a nested-args batch and picking Edit does not mount any nested-editable controls beyond what the whitelist allows', async () => {
    const user = userEvent.setup();
    render(<ToolApprovalCard batch={nestedArgsBatch} onSubmit={vi.fn()} />);
    await user.click(screen.getByRole('button', { name: /Изменить tool_with_nested_args/ }));
    expect(screen.getByLabelText('Текст')).toBeInTheDocument();
    expect(screen.queryByLabelText(/meta/i)).not.toBeInTheDocument();
    expect(screen.queryByLabelText(/author/i)).not.toBeInTheDocument();
  });

  it('an empty editedArgs map produces a payload entry without an edited_args key', async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    render(<ToolApprovalCard batch={singleCallBatch} onSubmit={onSubmit} />);

    await user.click(screen.getByRole('button', { name: /Изменить telegram__send_channel_post/ }));
    await user.click(screen.getByRole('button', { name: /^Подтвердить$/ }));

    expect(onSubmit).toHaveBeenCalledTimes(1);
    const decisions = onSubmit.mock.calls[0]![0];
    expect(decisions[0].action).toBe('edit');
    expect('edited_args' in decisions[0]).toBe(false);
    void act;
  });
});
