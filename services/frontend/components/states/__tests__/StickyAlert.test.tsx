import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import { StickyAlert } from '../StickyAlert';

describe('StickyAlert', () => {
  it('renders title and description with role="status"', () => {
    render(
      <StickyAlert
        title="VK потерял авторизацию"
        description="Сообщения за последние 12 часов могут быть пропущены."
      />
    );
    const status = screen.getByRole('status');
    expect(status).toHaveTextContent('VK потерял авторизацию');
    expect(status).toHaveTextContent(/Сообщения за последние 12 часов/);
    expect(status).toHaveAttribute('aria-live', 'polite');
  });

  it('invokes the action callback when the action button is clicked', async () => {
    const onClick = vi.fn();
    const user = userEvent.setup();
    render(
      <StickyAlert
        title="VK потерял авторизацию"
        action={{ label: 'Переподключить', onClick }}
      />
    );
    await user.click(screen.getByRole('button', { name: 'Переподключить' }));
    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it('renders dismiss button only when onDismiss is provided', async () => {
    const onDismiss = vi.fn();
    const user = userEvent.setup();
    const { rerender } = render(<StickyAlert title="Heads up" />);
    expect(
      screen.queryByRole('button', { name: 'Скрыть уведомление' })
    ).not.toBeInTheDocument();

    rerender(<StickyAlert title="Heads up" onDismiss={onDismiss} />);
    await user.click(screen.getByRole('button', { name: 'Скрыть уведомление' }));
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });

  it('applies the warning tone classes by default and switches when tone="danger"', () => {
    const { rerender } = render(<StickyAlert title="Heads up" />);
    expect(screen.getByRole('status').className).toMatch(/bg-warning-soft/);

    rerender(<StickyAlert title="Heads up" tone="danger" />);
    expect(screen.getByRole('status').className).toMatch(/bg-\[var\(--ov-danger-soft\)\]/);
  });
});
