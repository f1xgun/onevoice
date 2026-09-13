import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import { ToolApprovalArgsForm } from '../ToolApprovalArgsForm';

// Locked-section heading copy from messages/ru.json. Hard-coded here so a
// drift in the catalog without intent gets caught.
const LOCKED_HEADING = 'Выбрано автоматически';
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
    const sw = screen.getByRole('switch', { name: /Дополнительные данные/i });
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
    await user.click(screen.getByRole('switch', { name: /Дополнительные данные/i }));
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
    expect(screen.getByRole('switch', { name: /Дополнительные данные/i })).toBeDisabled();
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
    const lastCall = onEdit.mock.calls.at(-1);
    expect(lastCall?.[0]).toBe('count');
    expect(lastCall?.[1]).toBe(42);
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
    await user.type(input, '3.5');
    const numericCalls = onEdit.mock.calls.filter(([, v]) => typeof v === 'number');
    for (const [, v] of numericCalls) {
      expect(Number.isInteger(v)).toBe(true);
    }
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

  it('hides unknown read-only keys and values', () => {
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
    expect(screen.queryByText('definitely_unknown_key')).not.toBeInTheDocument();
    expect(screen.queryByText('value')).not.toBeInTheDocument();
    expect(screen.getByText('Для этого действия дополнительных данных нет.')).toBeInTheDocument();
  });
});

describe('ToolApprovalArgsForm — hidden internal values', () => {
  it('does not render nested routing data or JSON', () => {
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
    expect(screen.queryByText(LOCKED_HEADING)).not.toBeInTheDocument();
    expect(screen.queryByText('inner')).not.toBeInTheDocument();
    expect(screen.queryByText('tg-1')).not.toBeInTheDocument();
    expect(screen.queryByText(/^\{.+\}$/)).not.toBeInTheDocument();
    expect(screen.getByText('Для этого действия дополнительных данных нет.')).toBeInTheDocument();
  });

  it('does not render unknown arrays', () => {
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
    expect(screen.queryByText('news')).not.toBeInTheDocument();
    expect(screen.queryByText('promo')).not.toBeInTheDocument();
  });

  it('does not render unknown read-only flags', () => {
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
    expect(screen.queryByText('Да')).not.toBeInTheDocument();
    expect(screen.queryByText('Нет')).not.toBeInTheDocument();
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
    expect(screen.getByText('Для этого действия дополнительных данных нет.')).toBeInTheDocument();
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
