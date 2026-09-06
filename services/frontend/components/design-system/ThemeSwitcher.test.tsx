import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { toast } from 'sonner';
import { ThemeSwitcher } from './ThemeSwitcher';
import { ThemeProvider } from './ThemeProvider';
import ru from '@/messages/ru.json';
import en from '@/messages/en.json';
const refresh = vi.hoisted(() => vi.fn());
vi.mock('next/navigation', () => ({ useRouter: () => ({ refresh }) }));
vi.mock('sonner', () => ({ toast: { error: vi.fn() } }));
beforeEach(() => {
  vi.clearAllMocks();
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(null, { status: 204 }));
});
afterEach(() => vi.restoreAllMocks());

it('has matching localized keys', () =>
  expect(Object.keys(ru.theme).sort()).toEqual(Object.keys(en.theme).sort()));
it('supports keyboard selection and refreshes after saving', async () => {
  const user = userEvent.setup();
  render(<ThemeSwitcher />);
  await user.tab();
  expect(screen.getByRole('button', { name: 'Тема' })).toHaveFocus();
  await user.keyboard('{Enter}');
  expect(screen.getByRole('menuitemradio', { name: 'Системная' })).toHaveAttribute(
    'aria-checked',
    'true'
  );
  await user.keyboard('{End}{Enter}');
  await waitFor(() => expect(refresh).toHaveBeenCalledOnce());
  expect(fetch).toHaveBeenCalledWith(
    '/api/theme',
    expect.objectContaining({ method: 'POST', body: JSON.stringify({ theme: 'dark' }) })
  );
});
it('blocks repeat requests while saving and follows refreshed server props', async () => {
  let finish!: (response: Response) => void;
  vi.mocked(fetch).mockReturnValue(
    new Promise((resolve) => {
      finish = resolve;
    })
  );
  const user = userEvent.setup();
  const { rerender } = render(
    <ThemeProvider theme="system">
      <ThemeSwitcher />
    </ThemeProvider>
  );
  await user.click(screen.getByRole('button', { name: 'Тема' }));
  await user.click(screen.getByRole('menuitemradio', { name: 'Тёмная' }));
  expect(screen.getByRole('button', { name: 'Тема' })).toBeDisabled();
  fireEvent.click(screen.getByRole('button', { name: 'Тема' }));
  expect(fetch).toHaveBeenCalledOnce();
  await act(async () => finish(new Response(null, { status: 204 })));
  rerender(
    <ThemeProvider theme="dark">
      <ThemeSwitcher />
    </ThemeProvider>
  );
  await user.click(screen.getByRole('button', { name: 'Тема' }));
  expect(screen.getByRole('menuitemradio', { name: 'Тёмная' })).toHaveAttribute(
    'aria-checked',
    'true'
  );
});
it.each(['http', 'network'])('restores selection on %s failure', async (failure) => {
  if (failure === 'http') vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 500 }));
  else vi.mocked(fetch).mockRejectedValue(new TypeError('offline'));
  const user = userEvent.setup();
  render(
    <ThemeProvider theme="light">
      <ThemeSwitcher />
    </ThemeProvider>
  );
  await user.click(screen.getByRole('button', { name: 'Тема' }));
  await user.click(screen.getByRole('menuitemradio', { name: 'Тёмная' }));
  await waitFor(() => expect(toast.error).toHaveBeenCalledWith(ru.theme.saveFailed));
  expect(refresh).not.toHaveBeenCalled();
  await user.click(screen.getByRole('button', { name: 'Тема' }));
  expect(screen.getByRole('menuitemradio', { name: 'Светлая' })).toHaveAttribute(
    'aria-checked',
    'true'
  );
});
it('does not save the current preference', async () => {
  const user = userEvent.setup();
  render(<ThemeSwitcher />);
  await user.click(screen.getByRole('button', { name: 'Тема' }));
  await user.click(screen.getByRole('menuitemradio', { name: 'Системная' }));
  expect(fetch).not.toHaveBeenCalled();
});
