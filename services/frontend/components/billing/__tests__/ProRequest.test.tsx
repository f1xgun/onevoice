import { beforeEach, expect, it, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ProRequest } from '../ProRequest';
import { api } from '@/lib/api';
import { joinWaitlist } from '@/lib/api/waitlist';
import { API_PATHS } from '@/lib/constants/apiPaths';
import ru from '@/messages/ru.json';

vi.mock('@/lib/api', async () => {
  const { default: axios } = await import('axios');
  return { api: axios.create() };
});
const adapter = vi.fn();
beforeEach(() => {
  adapter.mockReset().mockImplementation(async (config) => ({
    status: 204,
    data: '',
    headers: {},
    statusText: 'No Content',
    config,
  }));
  api.defaults.adapter = adapter;
});

it.each([
  ['billing', 'new@example.org'],
  ['business-limit', 'new@example.org'],
  ['billing', 'existing@example.org'],
  ['business-limit', 'existing@example.org'],
] as const)(
  'submits Pro intent from %s for %s only with explicit consent',
  async (source, email) => {
    if (email === 'existing@example.org') {
      await joinWaitlist({ email, consent: true, source: 'landing' });
      expect(JSON.parse(adapter.mock.calls[0][0].data)).toEqual({
        email,
        consent: true,
        source: 'landing',
      });
      adapter.mockClear();
    }
    const user = userEvent.setup();
    render(<ProRequest source={source} />);
    await user.click(screen.getByRole('button', { name: 'Оставить заявку' }));
    expect(screen.getByRole('dialog')).toHaveAccessibleDescription(
      ru.settings.billing.pro.description
    );
    const dialog = within(screen.getByRole('dialog'));
    await user.type(dialog.getByRole('textbox'), email);
    expect(dialog.getByRole('button', { name: 'Оставить заявку' })).toBeDisabled();
    expect(adapter).not.toHaveBeenCalled();
    await user.click(dialog.getByRole('checkbox'));
    await user.click(dialog.getByRole('button', { name: 'Оставить заявку' }));
    expect(adapter).toHaveBeenCalledTimes(1);
    const request = adapter.mock.calls[0][0];
    expect(request.url).toBe(API_PATHS.WAITLIST);
    expect(request.method).toBe('post');
    expect(JSON.parse(request.data)).toEqual({
      email: email,
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
