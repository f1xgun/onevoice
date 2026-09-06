import '@testing-library/jest-dom/vitest';
import { createRef } from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { ActionButton } from './ActionButton';
import { AppDialog, Dialog, DialogTrigger, DialogTitle, DialogDescription } from './AppDialog';
import { AppCalendar } from './AppCalendar';
import { StatusLine } from './StatusLine';
import { CircleHelp } from 'lucide-react';

describe('design system interactions', () => {
  it('forwards refs and submits once; disabled actions cannot submit', async () => {
    const user = userEvent.setup();
    const submit = vi.fn(function onSubmit(event) {
      event.preventDefault();
    });
    const ref = createRef<HTMLButtonElement>();
    const { rerender } = render(
      <form onSubmit={submit}>
        <ActionButton ref={ref}>Разрешить</ActionButton>
      </form>
    );
    expect(ref.current).toBe(screen.getByRole('button'));
    await user.click(ref.current!);
    expect(submit).toHaveBeenCalledTimes(1);
    rerender(
      <form onSubmit={submit}>
        <ActionButton disabled>Разрешить</ActionButton>
      </form>
    );
    await user.click(screen.getByRole('button'));
    expect(submit).toHaveBeenCalledTimes(1);
  });

  it('preserves asChild link semantics and tracking attributes', () => {
    render(
      <ActionButton asChild>
        <a href="#waitlist" data-cta="hero-waitlist">
          Лист ожидания
        </a>
      </ActionButton>
    );
    expect(screen.getByRole('link')).toHaveAttribute('href', '#waitlist');
    expect(screen.getByRole('link')).toHaveAttribute('data-cta', 'hero-waitlist');
    expect(screen.queryByRole('button')).not.toBeInTheDocument();
  });

  it('blocks disabled slotted links before child handlers and preserves advisory aria-disabled actions', async () => {
    const user = userEvent.setup();
    const childClick = vi.fn();
    const { rerender } = render(
      <ActionButton asChild disabled>
        <a href="#waitlist" onClick={childClick}>
          Вступить
        </a>
      </ActionButton>
    );
    fireEvent.click(screen.getByRole('link'));
    expect(childClick).not.toHaveBeenCalled();
    expect(screen.getByRole('link')).toHaveAttribute('tabindex', '-1');
    rerender(
      <ActionButton aria-disabled onClick={childClick}>
        Проверьте решение
      </ActionButton>
    );
    await user.click(screen.getByRole('button'));
    expect(childClick).toHaveBeenCalledTimes(1);
  });

  it('keeps dialog title, description, Escape and return focus', async () => {
    const user = userEvent.setup();
    render(
      <Dialog>
        <DialogTrigger asChild>
          <ActionButton>Открыть</ActionButton>
        </DialogTrigger>
        <AppDialog>
          <div>
            <DialogTitle>Решение</DialogTitle>
            <DialogDescription>Проверьте текст</DialogDescription>
          </div>
          <div>
            <input aria-label="Текст" />
          </div>
          <div>
            <ActionButton>Разрешить</ActionButton>
          </div>
        </AppDialog>
      </Dialog>
    );
    await user.click(screen.getByRole('button', { name: 'Открыть' }));
    expect(screen.getByRole('dialog')).toHaveAccessibleName('Решение');
    expect(screen.getByRole('dialog')).toHaveAccessibleDescription('Проверьте текст');
    await user.keyboard('{Escape}');
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Открыть' })).toHaveFocus();
  });

  it('calendar selects dates and preserves disabled date semantics', () => {
    const select = vi.fn();
    render(
      <AppCalendar
        mode="single"
        defaultMonth={new Date(2026, 8, 1)}
        disabled={new Date(2026, 8, 3)}
        onSelect={select}
      />
    );
    const blocked = screen.getByRole('button', { name: /September 3rd/ });
    expect(blocked).toBeDisabled();
    fireEvent.click(blocked);
    expect(select).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: /September 4th/ }));
    expect(select).toHaveBeenCalledTimes(1);
  });

  it('selects a controlled range and marks both endpoints and intervening days', async () => {
    const user = userEvent.setup();
    const select = vi.fn();
    const from = new Date(2026, 8, 4);
    const to = new Date(2026, 8, 7);
    const { rerender } = render(<AppCalendar mode="range" defaultMonth={from} onSelect={select} />);
    await user.click(screen.getByRole('button', { name: /September 4th/ }));
    expect(select.mock.lastCall?.[0]?.from).toEqual(from);
    rerender(
      <AppCalendar
        mode="range"
        defaultMonth={from}
        selected={select.mock.lastCall?.[0]}
        onSelect={select}
      />
    );
    await user.click(screen.getByRole('button', { name: /September 7th/ }));
    expect(select).toHaveBeenCalledTimes(2);
    expect(select.mock.lastCall?.[0]).toEqual({ from, to });
    rerender(
      <AppCalendar
        mode="range"
        defaultMonth={from}
        selected={select.mock.lastCall?.[0]}
        onSelect={select}
      />
    );
    for (const day of ['4th', '5th', '6th', '7th']) {
      expect(screen.getByRole('button', { name: new RegExp(`September ${day}`) })).toHaveAttribute(
        'aria-pressed',
        'true'
      );
    }
    expect(screen.getByRole('button', { name: /September 8th/ })).toHaveAttribute(
      'aria-pressed',
      'false'
    );
  });

  it('moves range focus across months, skips disabled days and selects with the keyboard', async () => {
    const user = userEvent.setup();
    const select = vi.fn();
    const from = new Date(2026, 8, 30);
    render(
      <AppCalendar
        mode="range"
        defaultMonth={from}
        selected={{ from }}
        disabled={new Date(2026, 9, 2)}
        onSelect={select}
      />
    );
    await user.tab();
    await user.tab();
    await user.tab();
    expect(screen.getByRole('button', { name: /September 30th/ })).toHaveFocus();
    await user.keyboard('{ArrowRight}');
    expect(screen.getByRole('button', { name: /October 1st/ })).toHaveFocus();
    await user.keyboard('{ArrowRight}');
    expect(screen.getByRole('button', { name: /October 3rd/ })).toHaveFocus();
    await user.keyboard('{ArrowDown}');
    expect(screen.getByRole('button', { name: /October 10th/ })).toHaveFocus();
    expect(select).not.toHaveBeenCalled();
    await user.keyboard('{Enter}');
    expect(select).toHaveBeenCalledTimes(1);
    expect(select.mock.lastCall?.[0]).toEqual({ from, to: new Date(2026, 9, 10) });
  });

  it('announces supplied unknown-outcome text without inferring success', () => {
    render(
      <StatusLine
        role="status"
        tone="warning"
        icon={CircleHelp}
        text="Не удалось проверить отправку"
      />
    );
    expect(screen.getByRole('status')).toHaveTextContent('Не удалось проверить отправку');
    expect(screen.getByRole('status').querySelector('svg')).toHaveAttribute('aria-hidden', 'true');
  });
});
