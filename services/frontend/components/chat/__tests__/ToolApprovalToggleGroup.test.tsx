import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ToolApprovalToggleGroup } from '../ToolApprovalToggleGroup';

// Shared default props — each test overrides only what it needs.
function renderGroup(
  overrides: Partial<React.ComponentProps<typeof ToolApprovalToggleGroup>> = {}
) {
  const onSelect = vi.fn();
  const utils = render(
    <ToolApprovalToggleGroup
      toolName="telegram__send_channel_post"
      decision="undecided"
      onSelect={onSelect}
      {...overrides}
    />
  );
  return { onSelect, ...utils };
}

describe('ToolApprovalToggleGroup', () => {
  it('M) renders three buttons with the exact Russian labels', () => {
    renderGroup();
    expect(screen.getByRole('button', { name: /Одобрить/ })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Изменить/ })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Отклонить/ })).toBeInTheDocument();
  });

  it('N) marks exactly one button with aria-pressed="true" when decision is set', () => {
    renderGroup({ decision: 'approve' });
    expect(screen.getByRole('button', { name: /Одобрить/ })).toHaveAttribute(
      'aria-pressed',
      'true'
    );
    expect(screen.getByRole('button', { name: /Изменить/ })).toHaveAttribute(
      'aria-pressed',
      'false'
    );
    expect(screen.getByRole('button', { name: /Отклонить/ })).toHaveAttribute(
      'aria-pressed',
      'false'
    );
  });

  it('O) marks every button aria-pressed="false" when decision is "undecided"', () => {
    renderGroup({ decision: 'undecided' });
    for (const label of [/Одобрить/, /Изменить/, /Отклонить/]) {
      expect(screen.getByRole('button', { name: label })).toHaveAttribute('aria-pressed', 'false');
    }
  });

  it('P) clicking the Edit button fires onSelect with "edit" exactly once', async () => {
    const user = userEvent.setup();
    const { onSelect } = renderGroup();
    await user.click(screen.getByRole('button', { name: /Изменить/ }));
    expect(onSelect).toHaveBeenCalledTimes(1);
    expect(onSelect).toHaveBeenCalledWith('edit');
  });

  it('Q) disables all three buttons when disabled={true}', () => {
    renderGroup({ disabled: true });
    for (const label of [/Одобрить/, /Изменить/, /Отклонить/]) {
      expect(screen.getByRole('button', { name: label })).toBeDisabled();
    }
  });

  it('R) aria-label on every button contains the toolName', () => {
    renderGroup({ toolName: 'telegram__send_channel_post' });
    expect(screen.getByRole('button', { name: /Одобрить/ })).toHaveAttribute(
      'aria-label',
      'Одобрить telegram__send_channel_post'
    );
    expect(screen.getByRole('button', { name: /Изменить/ })).toHaveAttribute(
      'aria-label',
      'Изменить telegram__send_channel_post'
    );
    expect(screen.getByRole('button', { name: /Отклонить/ })).toHaveAttribute(
      'aria-label',
      'Отклонить telegram__send_channel_post'
    );
  });

  it('S) dims inactive siblings via opacity-60 when a decision is set', () => {
    renderGroup({ decision: 'approve' });
    const editBtn = screen.getByRole('button', { name: /Изменить/ });
    const rejectBtn = screen.getByRole('button', { name: /Отклонить/ });
    expect(editBtn.className).toContain('opacity-60');
    expect(rejectBtn.className).toContain('opacity-60');
  });

  it('T) Space keyboard activation fires onSelect with the focused action', async () => {
    const user = userEvent.setup();
    const { onSelect } = renderGroup({ toolName: 'foo' });
    const approveBtn = screen.getByRole('button', { name: /Одобрить foo/ });
    approveBtn.focus();
    await user.keyboard(' ');
    expect(onSelect).toHaveBeenCalledWith('approve');
  });
});
