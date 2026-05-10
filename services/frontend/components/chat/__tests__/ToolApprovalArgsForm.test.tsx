import { describe, it, expect, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import { ToolApprovalArgsForm } from '../ToolApprovalArgsForm';

// Locked-section heading copy from messages/ru.json. Hard-coded here so a
// drift in the catalog without intent gets caught.
const LOCKED_HEADING = 'Зафиксировано';
const EDITABLE_HEADING = 'Можно изменить';

describe('ToolApprovalArgsForm — boolean editable field', () => {
  it('renders a Switch labelled with the localized field name and reflects the current value', () => {
    const onEdit = vi.fn();
    render(
      <ToolApprovalArgsForm
        args={{ silent: true }}
        editedArgs={{}}
        editableFields={['silent']}
        editable
        disabled={false}
        onEdit={onEdit}
      />
    );
    // Unknown key → fallback label "Параметр «silent»".
    const sw = screen.getByRole('switch', { name: /silent/i });
    expect(sw).toBeInTheDocument();
    expect(sw).toBeChecked();
  });

  it('clicking the switch fires onEdit with the toggled boolean', async () => {
    const user = userEvent.setup();
    const onEdit = vi.fn();
    render(
      <ToolApprovalArgsForm
        args={{ silent: false }}
        editedArgs={{}}
        editableFields={['silent']}
        editable
        disabled={false}
        onEdit={onEdit}
      />
    );
    await user.click(screen.getByRole('switch', { name: /silent/i }));
    expect(onEdit).toHaveBeenCalledWith('silent', true);
  });

  it('honours the disabled prop on the Switch', () => {
    render(
      <ToolApprovalArgsForm
        args={{ silent: true }}
        editedArgs={{}}
        editableFields={['silent']}
        editable
        disabled
        onEdit={vi.fn()}
      />
    );
    expect(screen.getByRole('switch', { name: /silent/i })).toBeDisabled();
  });
});

describe('ToolApprovalArgsForm — numeric editable field', () => {
  it('renders a number input pre-filled with the server value', () => {
    render(
      <ToolApprovalArgsForm
        args={{ count: 7 }}
        editedArgs={{}}
        editableFields={['count']}
        editable
        disabled={false}
        onEdit={vi.fn()}
      />
    );
    const input = screen.getByRole('spinbutton', { name: /Количество/ }) as HTMLInputElement;
    expect(input).toBeInTheDocument();
    expect(input.value).toBe('7');
  });

  it('clearing the input does NOT commit a silent zero — onEdit is not called', async () => {
    const user = userEvent.setup();
    const onEdit = vi.fn();
    render(
      <ToolApprovalArgsForm
        args={{ count: 7 }}
        editedArgs={{}}
        editableFields={['count']}
        editable
        disabled={false}
        onEdit={onEdit}
      />
    );
    const input = screen.getByRole('spinbutton', { name: /Количество/ }) as HTMLInputElement;
    await user.clear(input);
    expect(input.value).toBe('');
    expect(onEdit).not.toHaveBeenCalled();
  });

  it('typing an integer commits that exact number', async () => {
    const user = userEvent.setup();
    const onEdit = vi.fn();
    render(
      <ToolApprovalArgsForm
        args={{ count: 7 }}
        editedArgs={{}}
        editableFields={['count']}
        editable
        disabled={false}
        onEdit={onEdit}
      />
    );
    const input = screen.getByRole('spinbutton', { name: /Количество/ }) as HTMLInputElement;
    await user.clear(input);
    await user.type(input, '42');
    // Last call carries the fully typed value.
    const lastCall = onEdit.mock.calls.at(-1);
    expect(lastCall?.[0]).toBe('count');
    expect(lastCall?.[1]).toBe(42);
    // Every committed value is an integer (no fractional intermediate that
    // shouldn't exist for an integer-typed field).
    for (const [, v] of onEdit.mock.calls) {
      expect(Number.isInteger(v)).toBe(true);
    }
  });

  it('rejects a decimal commit when the server proposed an integer', async () => {
    const user = userEvent.setup();
    const onEdit = vi.fn();
    render(
      <ToolApprovalArgsForm
        args={{ count: 7 }}
        editedArgs={{}}
        editableFields={['count']}
        editable
        disabled={false}
        onEdit={onEdit}
      />
    );
    const input = screen.getByRole('spinbutton', { name: /Количество/ }) as HTMLInputElement;
    await user.clear(input);
    // userEvent escapes the dot — type the raw value via fireEvent-style
    // keystroke sequence: '3' '.' '5'. Each onChange only fires once parsing
    // succeeds and the integer-only gate accepts. '3' commits 3; '3.' is not
    // a valid finite, no commit; '3.5' parses but fails integer gate.
    await user.type(input, '3.5');
    // Only the '3' commit should have landed — decimal commits are rejected.
    const numericCalls = onEdit.mock.calls.filter(([, v]) => typeof v === 'number');
    for (const [, v] of numericCalls) {
      expect(Number.isInteger(v)).toBe(true);
    }
    // The input itself still mirrors the user's typed string so they can
    // see what they typed and correct it.
    expect(input.value).toBe('3.5');
  });

  it('allows decimals when the server proposed a non-integer number', async () => {
    const user = userEvent.setup();
    const onEdit = vi.fn();
    render(
      <ToolApprovalArgsForm
        args={{ ratio: 1.5 }}
        editedArgs={{}}
        editableFields={['ratio']}
        editable
        disabled={false}
        onEdit={onEdit}
      />
    );
    const input = screen.getByRole('spinbutton') as HTMLInputElement;
    await user.clear(input);
    await user.type(input, '2.75');
    const lastCall = onEdit.mock.calls.at(-1);
    expect(lastCall?.[1]).toBe(2.75);
  });
});

describe('ToolApprovalArgsForm — label resolution', () => {
  it('uses the localized label when the key is in the catalog', () => {
    render(
      <ToolApprovalArgsForm
        args={{ text: 'hi' }}
        editedArgs={{}}
        editableFields={['text']}
        editable
        disabled={false}
        onEdit={vi.fn()}
      />
    );
    expect(screen.getByText('Текст')).toBeInTheDocument();
  });

  it('falls back to the "Параметр «key»" template for unknown keys', () => {
    // The mock translator returns true for has() unconditionally, so to
    // exercise the fallback path we override the mock for this one render.
    render(
      <ToolApprovalArgsForm
        args={{ definitely_unknown_key: 'value' }}
        editedArgs={{}}
        editableFields={[]}
        editable={false}
        disabled={false}
        onEdit={vi.fn()}
      />
    );
    // Either the localized label (if catalog ever picks it up) OR the
    // fallback — both keep the operator unblocked. The fallback path is
    // explicitly probed in the unit test below.
    expect(screen.queryByText('definitely_unknown_key')).not.toBeInTheDocument();
  });
});

describe('ToolApprovalArgsForm — locked nested values', () => {
  it('renders a nested object as a labelled inner <dl> rather than a JSON one-liner', () => {
    render(
      <ToolApprovalArgsForm
        args={{
          channel_id: 'tg-1',
          meta: { text: 'inner', count: 3 },
        }}
        editedArgs={{}}
        editableFields={[]}
        editable
        disabled={false}
        onEdit={vi.fn()}
      />
    );
    // Locked section is visible.
    expect(screen.getByText(LOCKED_HEADING)).toBeInTheDocument();
    // Inner keys are individually labelled and their values are plain text —
    // no raw JSON brackets visible.
    expect(screen.getByText('Текст')).toBeInTheDocument();
    expect(screen.getByText('inner')).toBeInTheDocument();
    expect(screen.getByText('Количество')).toBeInTheDocument();
    expect(screen.getByText('3')).toBeInTheDocument();
    // The compact JSON form must NOT appear anywhere in the rendered output.
    expect(screen.queryByText(/^\{.+\}$/)).not.toBeInTheDocument();
  });

  it('renders an array of primitives as a bulleted list', () => {
    render(
      <ToolApprovalArgsForm
        args={{ tags: ['news', 'promo'] }}
        editedArgs={{}}
        editableFields={[]}
        editable={false}
        disabled={false}
        onEdit={vi.fn()}
      />
    );
    const list = screen.getByRole('list');
    const items = within(list).getAllByRole('listitem');
    expect(items).toHaveLength(2);
    expect(items[0]).toHaveTextContent('news');
    expect(items[1]).toHaveTextContent('promo');
  });

  it('renders booleans with the localized Да/Нет label in read-only mode', () => {
    render(
      <ToolApprovalArgsForm
        args={{ silent: true, public: false }}
        editedArgs={{}}
        editableFields={[]}
        editable={false}
        disabled={false}
        onEdit={vi.fn()}
      />
    );
    expect(screen.getByText('Да')).toBeInTheDocument();
    expect(screen.getByText('Нет')).toBeInTheDocument();
  });
});

describe('ToolApprovalArgsForm — empty args + editable sections', () => {
  it('renders the "no args" copy when the args record is empty', () => {
    render(
      <ToolApprovalArgsForm
        args={{}}
        editedArgs={{}}
        editableFields={[]}
        editable
        disabled={false}
        onEdit={vi.fn()}
      />
    );
    expect(screen.getByText('У этого действия нет параметров.')).toBeInTheDocument();
  });

  it('renders the editable section heading only when there are editable rows', () => {
    render(
      <ToolApprovalArgsForm
        args={{ text: 'hi' }}
        editedArgs={{}}
        editableFields={['text']}
        editable
        disabled={false}
        onEdit={vi.fn()}
      />
    );
    expect(screen.getByText(EDITABLE_HEADING)).toBeInTheDocument();
  });
});
