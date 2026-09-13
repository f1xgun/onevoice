import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import {
  ToolApprovalAccordionEntry,
  type AccordionEntryDraft,
  type ToolApprovalAccordionEntryProps,
} from '../ToolApprovalAccordionEntry';
import {
  noEditableFieldsBatch,
  singleCallBatch,
  threeCallBatch,
} from '@/test-utils/pending-approval-fixtures';

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

describe('ToolApprovalAccordionEntry — header + decision toggle', () => {
  it('renders readable platform and action names without the internal identifier', () => {
    renderEntry({ call: singleCallBatch.calls[0]! });
    expect(screen.getByText('Telegram')).toBeInTheDocument();
    expect(screen.getByText('Отправить пост')).toBeInTheDocument();
    expect(screen.queryByText('telegram__send_channel_post')).not.toBeInTheDocument();
  });

  it('each toggle button aria-label includes the readable action name', () => {
    renderEntry({ call: singleCallBatch.calls[0]! });
    expect(screen.getByRole('button', { name: /Одобрить Отправить пост/ })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Изменить Отправить пост/ })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Отклонить Отправить пост/ })).toBeInTheDocument();
  });

  it('starts expanded so the content to be approved is visible without a click', () => {
    renderEntry({ call: singleCallBatch.calls[0]! });
    expect(screen.getByLabelText(/Отправить пост — свернуть/)).toBeInTheDocument();
    expect(screen.getByText('hello')).toBeInTheDocument();
  });

  it('Space on the collapsible trigger toggles the body when the trigger is focusable', async () => {
    const user = userEvent.setup();
    renderEntry({ call: singleCallBatch.calls[0]! });
    const trigger = screen.getByLabelText(/Отправить пост — свернуть/);
    trigger.focus();
    await user.keyboard(' ');
    expect(screen.getByLabelText(/Отправить пост — развернуть/)).toBeInTheDocument();
  });

  it('uses a readable fallback for an unknown action', () => {
    const vkCall = threeCallBatch.calls[1]!;
    renderEntry({ call: vkCall });
    expect(screen.getByText('ВКонтакте')).toBeInTheDocument();
    expect(screen.getByText('Действие на площадке')).toBeInTheDocument();
    expect(screen.queryByText('vk__create_post')).not.toBeInTheDocument();
  });

  it('clicking Approve fires onSelectDecision with "approve"', async () => {
    const user = userEvent.setup();
    const onSelectDecision = vi.fn();
    renderEntry({ call: singleCallBatch.calls[0]!, onSelectDecision });
    await user.click(screen.getByRole('button', { name: /Одобрить/ }));
    expect(onSelectDecision).toHaveBeenCalledWith('approve');
  });

  it('applies ring-warning when amberHighlighted is true', () => {
    const { container } = renderEntry({
      call: singleCallBatch.calls[0]!,
      amberHighlighted: true,
    });
    const wrapper = container.firstChild as HTMLElement;
    expect(wrapper.className).toContain('ring-warning');
  });

  it('when a decision is selected, the collapsible trigger aria-label switches to "свернуть"', () => {
    renderEntry({
      call: singleCallBatch.calls[0]!,
      draft: makeDraft({ decision: 'edit' }),
    });
    expect(screen.getByLabelText(/Отправить пост — свернуть/)).toBeInTheDocument();
  });
});

describe('ToolApprovalAccordionEntry — reject textarea + counter', () => {
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

  it('counter gets text-destructive class once the reject reason crosses 500 chars', () => {
    const overflowing = 'a'.repeat(501);
    renderEntry({
      call: singleCallBatch.calls[0]!,
      draft: makeDraft({ decision: 'reject', rejectReason: overflowing }),
    });
    const counter = screen.getByText('501 / 500');
    expect(counter.className).toContain('text-destructive');
  });
});

describe('ToolApprovalAccordionEntry — args form (read-only modes)', () => {
  it('shows action details and hides routing identifiers', () => {
    renderEntry({
      call: singleCallBatch.calls[0]!, // args: { chat_id: 123, text: 'hello' }
      draft: makeDraft({ decision: 'undecided' }),
    });
    expect(screen.getByText('Что произойдёт')).toBeInTheDocument();
    expect(screen.getByText('Текст')).toBeInTheDocument();
    expect(screen.queryByText('ID чата')).not.toBeInTheDocument();
    expect(screen.getByText('hello')).toBeInTheDocument();
    expect(screen.queryByText('123')).not.toBeInTheDocument();
  });

  it('renders args read-only when decision is approve (publish tool starts expanded)', () => {
    renderEntry({
      call: singleCallBatch.calls[0]!,
      draft: makeDraft({ decision: 'approve' }),
    });
    expect(screen.getByText('Текст')).toBeInTheDocument();
    expect(screen.getByText('hello')).toBeInTheDocument();
    expect(screen.queryByRole('textbox', { name: 'Текст' })).not.toBeInTheDocument();
  });
});

describe('ToolApprovalAccordionEntry — args form (edit mode)', () => {
  it('renders a textarea pre-filled with the current text value for an editable string field', () => {
    renderEntry({
      call: singleCallBatch.calls[0]!, // editableFields: ['text', 'parse_mode']
      draft: makeDraft({ decision: 'edit' }),
    });
    expect(screen.getByText('Что произойдёт')).toBeInTheDocument();
    const textarea = screen.getByLabelText('Текст') as HTMLTextAreaElement;
    expect(textarea).toBeInTheDocument();
    expect(textarea.value).toBe('hello');
  });

  it('non-editable args render in the locked section with a heading + Lock icon hint', () => {
    renderEntry({
      call: singleCallBatch.calls[0]!,
      draft: makeDraft({ decision: 'edit' }),
    });
    expect(screen.getByText('Можно изменить')).toBeInTheDocument();
    expect(screen.queryByText('Выбрано автоматически')).not.toBeInTheDocument();
    expect(screen.queryByText('ID чата')).not.toBeInTheDocument();
  });

  it('typing into the editable textarea fires onEditArg with the new value', async () => {
    const user = userEvent.setup();
    const onEditArg = vi.fn();
    renderEntry({
      call: singleCallBatch.calls[0]!,
      draft: makeDraft({ decision: 'edit' }),
      onEditArg,
    });
    const textarea = screen.getByLabelText('Текст') as HTMLTextAreaElement;
    await user.type(textarea, '!');
    expect(onEditArg).toHaveBeenCalled();
    const lastCall = onEditArg.mock.calls.at(-1);
    expect(lastCall?.[0]).toBe('text');
    expect(String(lastCall?.[1])).toContain('hello!');
  });

  it('renders the "no editable fields" hint when the whitelist is empty', () => {
    renderEntry({
      call: noEditableFieldsBatch.calls[0]!,
      draft: makeDraft({ decision: 'edit' }),
    });
    expect(screen.getByText('Для этого действия дополнительных данных нет.')).toBeInTheDocument();
    expect(screen.queryByText('ID чата')).not.toBeInTheDocument();
    expect(screen.queryByText('ID сообщения')).not.toBeInTheDocument();
  });
});
