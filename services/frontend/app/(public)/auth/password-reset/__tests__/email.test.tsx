import { it, expect, vi, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import type { ReactNode } from 'react';
import PasswordResetRequestPage from '../page';

vi.mock('@/components/auth/AuthShell', () => ({
  AuthShell: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));
afterEach(() => vi.unstubAllGlobals());

it('submits a trimmed lowercase email for password recovery', async () => {
  const fetch = vi.fn().mockResolvedValue({ status: 204 });
  vi.stubGlobal('fetch', fetch);
  render(<PasswordResetRequestPage />);
  const input = screen.getByRole('textbox');
  fireEvent.change(input, { target: { value: '  Owner@Example.COM  ' } });
  fireEvent.submit(input.closest('form')!);
  await waitFor(() =>
    expect(fetch).toHaveBeenCalledWith(
      '/api/v1/auth/password-reset/request',
      expect.objectContaining({ body: JSON.stringify({ email: 'owner@example.com' }) })
    )
  );
});
