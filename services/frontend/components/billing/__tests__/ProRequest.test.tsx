import { beforeEach, expect, it, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ProRequest } from '../ProRequest';
import { joinWaitlist } from '@/lib/api/waitlist';
import ru from '@/messages/ru.json';

vi.mock('@/lib/api/waitlist', () => ({ joinWaitlist: vi.fn() }));
beforeEach(() => {
  vi.mocked(joinWaitlist).mockReset().mockResolvedValue();
});

it.each(['billing', 'business_limit'] as const)(
  'submits Pro intent from %s only with explicit consent',
  async (source) => {
    const user = userEvent.setup();
    render(<ProRequest source={source} />);
    await user.click(screen.getByRole('button', { name: 'Оставить заявку' }));
    const dialog = within(screen.getByRole('dialog'));
    await user.type(dialog.getByRole('textbox'), 'owner@example.org');
    expect(dialog.getByRole('button', { name: 'Оставить заявку' })).toBeDisabled();
    expect(joinWaitlist).not.toHaveBeenCalled();
    await user.click(dialog.getByRole('checkbox'));
    await user.click(dialog.getByRole('button', { name: 'Оставить заявку' }));
    expect(joinWaitlist).toHaveBeenCalledWith({
      email: 'owner@example.org',
      consent: true,
      source,
      plan: 'pro',
    });
    await dialog.findByText(ru.landing.waitlist.success.title);
    expect(
      dialog.queryByRole('link', { name: ru.landing.waitlist.success.registerCta })
    ).not.toBeInTheDocument();
  }
);
