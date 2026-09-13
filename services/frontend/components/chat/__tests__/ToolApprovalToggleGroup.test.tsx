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
      actionName="Отправить пост"
      decision="undecided"
      onSelect={onSelect}
      {...overrides}
    />
  );
  return { onSelect, ...utils };
}

describe('ToolApprovalToggleGroup', () => {
  it.each([
    { decision: 'approve' as const, label: /Одобрить/, color: 'text-ink' },
    { decision: 'edit' as const, label: /Изменить/, color: 'text-ink' },
    { decision: 'reject' as const, label: /Отклонить/, color: 'text-danger' },
  ])('renders the documented active styling for $decision', ({ decision, label, color }) => {
    renderGroup({ decision });
    const active = screen.getByRole('button', { name: label });
    expect(active).toHaveClass('bg-paper-raised', color, 'ring-2', 'ring-ring');
    expect(active).not.toHaveClass('bg-brand');
    for (const button of screen.getAllByRole('button')) {
      if (button === active) continue;
      expect(button).toHaveClass('bg-paper-raised', 'text-ink');
      expect(button).not.toHaveClass('ring-2');
    }
  });

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

  it('R) aria-label on every button contains the readable action name', () => {
    renderGroup({ actionName: 'Отправить пост' });
    expect(screen.getByRole('button', { name: /Одобрить/ })).toHaveAttribute(
      'aria-label',
      'Одобрить Отправить пост'
    );
    expect(screen.getByRole('button', { name: /Изменить/ })).toHaveAttribute(
      'aria-label',
      'Изменить Отправить пост'
    );
    expect(screen.getByRole('button', { name: /Отклонить/ })).toHaveAttribute(
      'aria-label',
      'Отклонить Отправить пост'
    );
  });

  it('keeps inactive decisions readable and exposes selection through aria-pressed', () => {
    renderGroup({ decision: 'approve' });
    const editBtn = screen.getByRole('button', { name: /Изменить/ });
    const rejectBtn = screen.getByRole('button', { name: /Отклонить/ });
    expect(editBtn.getAttribute('aria-pressed')).toBe('false');
    expect(editBtn.className).not.toContain('opacity-60');
    expect(rejectBtn.getAttribute('aria-pressed')).toBe('false');
    expect(rejectBtn.className).not.toContain('opacity-60');
  });

  it('T) Space keyboard activation fires onSelect with the focused action', async () => {
    const user = userEvent.setup();
    const { onSelect } = renderGroup({ actionName: 'Публикация' });
    const approveBtn = screen.getByRole('button', { name: /Одобрить Публикация/ });
    approveBtn.focus();
    await user.keyboard(' ');
    expect(onSelect).toHaveBeenCalledWith('approve');
  });
});
