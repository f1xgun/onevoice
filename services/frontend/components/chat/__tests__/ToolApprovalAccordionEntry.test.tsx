import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import {
  ToolApprovalAccordionEntry,
  type AccordionEntryDraft,
  type ToolApprovalAccordionEntryProps,
} from '../ToolApprovalAccordionEntry';
import { singleCallBatch, threeCallBatch } from '@/test-utils/pending-approval-fixtures';

function makeDraft(overrides: Partial<AccordionEntryDraft> = {}): AccordionEntryDraft {
  return {
    decision: 'undecided',
    editedArgs: {},
    rejectReason: '',
    ...overrides,
  };
}

function renderEntry(overrides: Partial<ToolApprovalAccordionEntryProps> = {}) {
  const call = overrides.call ?? singleCallBatch.calls[0]!;
  const onSelectDecision = vi.fn();
  const onEditArg = vi.fn();
  const onSetRejectReason = vi.fn();
  const props: ToolApprovalAccordionEntryProps = {
    call,
    draft: overrides.draft ?? makeDraft(),
    disabled: overrides.disabled ?? false,
    amberHighlighted: overrides.amberHighlighted ?? false,
    onSelectDecision: overrides.onSelectDecision ?? onSelectDecision,
    onEditArg: overrides.onEditArg ?? onEditArg,
    onSetRejectReason: overrides.onSetRejectReason ?? onSetRejectReason,
  };
  const utils = render(<ToolApprovalAccordionEntry {...props} />);
  return { ...utils, onSelectDecision, onEditArg, onSetRejectReason };
}

describe('ToolApprovalAccordionEntry', () => {
  it('HH) renders the platform badge (TG) and the monospaced tool name', () => {
    renderEntry({
      call: singleCallBatch.calls[0]!, // telegram__send_channel_post
    });
    expect(screen.getByText('TG')).toBeInTheDocument();
    expect(screen.getByText('telegram__send_channel_post')).toBeInTheDocument();
  });

  it('JJ) each toggle button aria-label includes the tool name', () => {
    renderEntry({ call: singleCallBatch.calls[0]! });
    expect(
      screen.getByRole('button', { name: /Одобрить telegram__send_channel_post/ })
    ).toBeInTheDocument();
    expect(
      screen.getByRole('button', { name: /Изменить telegram__send_channel_post/ })
    ).toBeInTheDocument();
    expect(
      screen.getByRole('button', { name: /Отклонить telegram__send_channel_post/ })
    ).toBeInTheDocument();
  });

  it('GG) Space on the collapsible trigger reveals the body when the trigger is focusable', async () => {
    const user = userEvent.setup();
    // Start with `undecided` so the body is collapsed by default; press
    // Space on the trigger to open it manually (Radix Collapsible keyboard
    // contract). The toggle group is always visible so the trigger IS
    // focusable via Tab through the header-row aria-label region.
    renderEntry({
      call: singleCallBatch.calls[0]!,
    });
    const trigger = screen.getByLabelText(/telegram__send_channel_post — развернуть/);
    trigger.focus();
    await user.keyboard(' ');
    // After opening, the aria-label flips to "свернуть" — proof the
    // collapsible toggled under keyboard control.
    expect(screen.getByLabelText(/telegram__send_channel_post — свернуть/)).toBeInTheDocument();
  });

  it('auto-expands when user picks Edit (decision set by parent)', () => {
    renderEntry({
      call: singleCallBatch.calls[0]!,
      draft: makeDraft({ decision: 'edit' }),
    });
    expect(screen.getByText('Аргументы')).toBeInTheDocument();
  });

  it('auto-expands when user picks Reject and renders the textarea with the placeholder', () => {
    renderEntry({
      call: singleCallBatch.calls[0]!,
      draft: makeDraft({ decision: 'reject' }),
    });
    expect(screen.getByPlaceholderText('Причина (необязательно)')).toBeInTheDocument();
    expect(screen.getByLabelText('Причина отказа')).toBeInTheDocument();
  });

  it('renders the 0 / 500 counter initially, then updates to the staged length', () => {
    const { rerender } = renderEntry({
      call: singleCallBatch.calls[0]!,
      draft: makeDraft({ decision: 'reject', rejectReason: '' }),
    });
    expect(screen.getByText('0 / 500')).toBeInTheDocument();
    // Re-render with a populated reason (parent-controlled).
    rerender(
      <ToolApprovalAccordionEntry
        call={singleCallBatch.calls[0]!}
        draft={makeDraft({ decision: 'reject', rejectReason: 'hello' })}
        disabled={false}
        amberHighlighted={false}
        onSelectDecision={vi.fn()}
        onEditArg={vi.fn()}
        onSetRejectReason={vi.fn()}
      />
    );
    expect(screen.getByText('5 / 500')).toBeInTheDocument();
  });

  it('II) counter gets text-destructive class once the reject reason crosses 500 chars', () => {
    // The parent reducer slices to 500, so the counter is expected to show
    // exactly 500 in practice. This test constructs the worst-case visual
    // guarantee (if the reducer were bypassed, the counter flags overflow).
    const overflowing = 'a'.repeat(501);
    renderEntry({
      call: singleCallBatch.calls[0]!,
      draft: makeDraft({ decision: 'reject', rejectReason: overflowing }),
    });
    const counter = screen.getByText('501 / 500');
    expect(counter.className).toContain('text-destructive');
  });

  it('applies ring-amber-400 when amberHighlighted is true', () => {
    const { container } = renderEntry({
      call: singleCallBatch.calls[0]!,
      amberHighlighted: true,
    });
    const wrapper = container.firstChild as HTMLElement;
    expect(wrapper.className).toContain('ring-amber-400');
  });

  it('platform badge renders VK for a vk__ tool', () => {
    const vkCall = threeCallBatch.calls[1]!; // vk__create_post
    renderEntry({ call: vkCall });
    expect(screen.getByText('VK')).toBeInTheDocument();
    expect(screen.getByText('vk__create_post')).toBeInTheDocument();
  });

  it('clicking Approve fires onSelectDecision with "approve"', async () => {
    const user = userEvent.setup();
    const onSelectDecision = vi.fn();
    renderEntry({
      call: singleCallBatch.calls[0]!,
      onSelectDecision,
    });
    await user.click(screen.getByRole('button', { name: /Одобрить/ }));
    expect(onSelectDecision).toHaveBeenCalledWith('approve');
  });

  it('editable-fields hint lists the allowlist when non-empty (Edit expansion)', () => {
    renderEntry({
      call: singleCallBatch.calls[0]!, // editableFields: ['text', 'parse_mode']
      draft: makeDraft({ decision: 'edit' }),
    });
    expect(screen.getByText(/Можно изменять:\s*text,\s*parse_mode/)).toBeInTheDocument();
  });

  it('when a decision is selected, the collapsible trigger aria-label switches to "свернуть"', () => {
    renderEntry({
      call: singleCallBatch.calls[0]!,
      draft: makeDraft({ decision: 'edit' }),
    });
    const trigger = screen.getByLabelText(/telegram__send_channel_post — свернуть/);
    expect(trigger).toBeInTheDocument();
  });

  // ── Plan 17-08 GAP-01 / GAP-02 closures ───────────────────────────────────
  //
  // GAP-01: operators must be able to read tool args BEFORE selecting a
  // decision. Pre-fix the `Аргументы` heading + JsonView only rendered when
  // `decision === 'edit'`, so Approve / undecided modes hid the args.
  //
  // GAP-02: in Edit mode, `@uiw/react-json-view/editor` requires double-click
  // on a value to invoke the inline editor — this is not discoverable. A
  // visible hint chip tells first-time operators how to edit.

  it('GAP-01: read-only Аргументы block + value visible when decision is undecided and entry is expanded', async () => {
    const user = userEvent.setup();
    renderEntry({
      call: singleCallBatch.calls[0]!, // args: { chat_id: 123, text: 'hello' }
      draft: makeDraft({ decision: 'undecided' }),
    });
    // Expand the entry (it is collapsed by default in 'undecided' mode).
    const trigger = screen.getByLabelText(/telegram__send_channel_post — развернуть/);
    await user.click(trigger);
    expect(screen.getByText('Аргументы')).toBeInTheDocument();
    // The args value (`hello`) must be present somewhere in the rendered
    // JSON tree. JsonView renders strings inside spans; queryAllByText with a
    // regex tolerant of surrounding quotes survives library escaping.
    expect(screen.getAllByText(/hello/i).length).toBeGreaterThan(0);
  });

  it('GAP-01: read-only Аргументы block + value visible when decision is approve and entry is expanded', async () => {
    const user = userEvent.setup();
    renderEntry({
      call: singleCallBatch.calls[0]!,
      draft: makeDraft({ decision: 'approve' }),
    });
    // Approve does not auto-expand (only Edit / Reject do); user must click.
    const trigger = screen.getByLabelText(/telegram__send_channel_post — развернуть/);
    await user.click(trigger);
    expect(screen.getByText('Аргументы')).toBeInTheDocument();
    expect(screen.getAllByText(/hello/i).length).toBeGreaterThan(0);
  });

  it('GAP-01 regression: Аргументы block stays visible in edit mode (existing behaviour preserved)', () => {
    renderEntry({
      call: singleCallBatch.calls[0]!,
      draft: makeDraft({ decision: 'edit' }),
    });
    expect(screen.getByText('Аргументы')).toBeInTheDocument();
  });

  it('GAP-02: edit-affordance hint chip renders in edit mode with the exact RU copy', () => {
    renderEntry({
      call: singleCallBatch.calls[0]!,
      draft: makeDraft({ decision: 'edit' }),
    });
    const hint = screen.getByTestId('edit-affordance-hint');
    expect(hint).toBeInTheDocument();
    expect(hint).toHaveTextContent('Дважды нажмите на значение, чтобы изменить');
  });

  it('GAP-02 negative: edit-affordance hint chip is NOT rendered in undecided mode', async () => {
    const user = userEvent.setup();
    renderEntry({
      call: singleCallBatch.calls[0]!,
      draft: makeDraft({ decision: 'undecided' }),
    });
    // Expand so the body is in the DOM — the chip must still be absent.
    const trigger = screen.getByLabelText(/telegram__send_channel_post — развернуть/);
    await user.click(trigger);
    expect(screen.queryByTestId('edit-affordance-hint')).not.toBeInTheDocument();
  });

  it('GAP-02 negative: edit-affordance hint chip is NOT rendered in approve mode', async () => {
    const user = userEvent.setup();
    renderEntry({
      call: singleCallBatch.calls[0]!,
      draft: makeDraft({ decision: 'approve' }),
    });
    const trigger = screen.getByLabelText(/telegram__send_channel_post — развернуть/);
    await user.click(trigger);
    expect(screen.queryByTestId('edit-affordance-hint')).not.toBeInTheDocument();
  });

  it('GAP-02 negative: edit-affordance hint chip is NOT rendered in reject mode (only the textarea)', () => {
    renderEntry({
      call: singleCallBatch.calls[0]!,
      draft: makeDraft({ decision: 'reject' }),
    });
    expect(screen.queryByTestId('edit-affordance-hint')).not.toBeInTheDocument();
    expect(screen.getByPlaceholderText('Причина (необязательно)')).toBeInTheDocument();
  });

  it('"Можно изменять" hint renders in BOTH read-only (undecided) and edit modes', async () => {
    const user = userEvent.setup();
    // Pass 1: undecided, must expand manually.
    const { unmount } = renderEntry({
      call: singleCallBatch.calls[0]!, // editableFields: ['text', 'parse_mode']
      draft: makeDraft({ decision: 'undecided' }),
    });
    await user.click(screen.getByLabelText(/telegram__send_channel_post — развернуть/));
    expect(screen.getByText(/Можно изменять:\s*text,\s*parse_mode/)).toBeInTheDocument();
    unmount();

    // Pass 2: edit (auto-expanded by useEffect).
    renderEntry({
      call: singleCallBatch.calls[0]!,
      draft: makeDraft({ decision: 'edit' }),
    });
    expect(screen.getByText(/Можно изменять:\s*text,\s*parse_mode/)).toBeInTheDocument();
  });
});
