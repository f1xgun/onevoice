import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, cleanup } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import SettingsPage from '../page';
import { useAuthStore } from '@/lib/auth';
import { api } from '@/lib/api';

vi.mock('@/lib/api', () => ({ api: { patch: vi.fn(), put: vi.fn() } }));
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

function wrapper({ children }: { children: ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

describe('SettingsPage — editable display name', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useAuthStore.setState({
      user: { id: 'u1', email: 'test@test.test', name: 'Old Name' },
      accessToken: 'tok',
      isAuthenticated: true,
    });
  });

  afterEach(() => cleanup());

  it('prefills the current name and saves a new one via PATCH /auth/profile', async () => {
    (api.patch as ReturnType<typeof vi.fn>).mockResolvedValue({ data: null });
    const user = userEvent.setup();
    render(<SettingsPage />, { wrapper });

    const input = screen.getByDisplayValue('Old Name');
    await user.clear(input);
    await user.type(input, 'New Name');
    await user.click(screen.getByRole('button', { name: 'Сохранить' }));

    await waitFor(() =>
      expect(api.patch).toHaveBeenCalledWith('/auth/profile', { name: 'New Name' })
    );
    await waitFor(() => expect(useAuthStore.getState().user?.name).toBe('New Name'));
  });

  it('blocks save and shows an error for a too-short name', async () => {
    const user = userEvent.setup();
    render(<SettingsPage />, { wrapper });

    const input = screen.getByDisplayValue('Old Name');
    await user.clear(input);
    await user.type(input, 'A');
    await user.click(screen.getByRole('button', { name: 'Сохранить' }));

    expect(await screen.findByText('Минимум 2 символа')).toBeInTheDocument();
    expect(api.patch).not.toHaveBeenCalled();
  });
});
