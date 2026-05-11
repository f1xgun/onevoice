import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { NextIntlClientProvider } from 'next-intl';
import type { ReactNode } from 'react';
import messages from '@/messages/ru.json';

const replace = vi.fn();
const push = vi.fn();

vi.mock('next/navigation', () => ({
  useRouter: () => ({ replace, push }),
  useParams: () => ({ token: 'abc123' }),
}));
vi.mock('@/lib/auth', () => ({ useAuthStore: vi.fn() }));
vi.mock('@/lib/hooks/useInvitations', () => ({
  useInvitationPreview: vi.fn(),
  useAcceptInvitation: vi.fn(),
}));
vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: vi.fn(),
}));
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() } }));

import { useAuthStore } from '@/lib/auth';
import { useInvitationPreview, useAcceptInvitation } from '@/lib/hooks/useInvitations';
import { useBusinessStore } from '@/lib/stores/business';
import { toast } from 'sonner';
import AcceptInvitePage from '../../../app/(public)/invite/[token]/page';

function wrap(node: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    <NextIntlClientProvider locale="ru" messages={messages}>
      <QueryClientProvider client={qc}>{node}</QueryClientProvider>
    </NextIntlClientProvider>
  );
}

const FRESH_PREVIEW = {
  business_id: 'biz-1',
  business_name: 'Acme',
  role_id: 'role-admin',
  role_name: 'admin',
  expires_at: '2026-05-17T00:00:00Z',
};

beforeEach(() => {
  replace.mockReset();
  push.mockReset();
  vi.mocked(toast.error).mockReset();
  vi.mocked(useBusinessStore).mockImplementation(
    (
      sel: (s: {
        activeBusinessId: string | null;
        setActive: (id: string | null) => void;
      }) => unknown
    ) => sel({ activeBusinessId: null, setActive: vi.fn() })
  );
});

describe('AcceptInvitePage', () => {
  it('redirects anonymous users to /login?next=/invite/{token}', () => {
    vi.mocked(useAuthStore).mockImplementation(
      (sel: (s: { isAuthenticated: boolean }) => unknown) => sel({ isAuthenticated: false })
    );
    vi.mocked(useInvitationPreview).mockReturnValue({ isLoading: true } as never);
    vi.mocked(useAcceptInvitation).mockReturnValue({
      mutateAsync: vi.fn(),
      isPending: false,
    } as never);
    render(wrap(<AcceptInvitePage />));
    expect(replace).toHaveBeenCalledWith('/login?next=/invite/abc123');
  });

  it('renders the preview card for authenticated users', async () => {
    vi.mocked(useAuthStore).mockImplementation(
      (sel: (s: { isAuthenticated: boolean }) => unknown) => sel({ isAuthenticated: true })
    );
    vi.mocked(useInvitationPreview).mockReturnValue({
      isLoading: false,
      isError: false,
      data: FRESH_PREVIEW,
    } as never);
    vi.mocked(useAcceptInvitation).mockReturnValue({
      mutateAsync: vi.fn(),
      isPending: false,
    } as never);
    render(wrap(<AcceptInvitePage />));
    await waitFor(() => expect(screen.getByText('Acme')).toBeInTheDocument());
    expect(screen.getByRole('button', { name: 'Принять приглашение' })).toBeInTheDocument();
  });

  it('on accept success, routes to /chat', async () => {
    vi.mocked(useAuthStore).mockImplementation(
      (sel: (s: { isAuthenticated: boolean }) => unknown) => sel({ isAuthenticated: true })
    );
    vi.mocked(useInvitationPreview).mockReturnValue({
      isLoading: false,
      isError: false,
      data: FRESH_PREVIEW,
    } as never);
    const mutateAsync = vi.fn().mockResolvedValue({ business_id: 'biz-1', role_id: 'role-admin' });
    vi.mocked(useAcceptInvitation).mockReturnValue({ mutateAsync, isPending: false } as never);
    render(wrap(<AcceptInvitePage />));
    await waitFor(() => screen.getByText('Acme'));
    fireEvent.click(screen.getByRole('button', { name: 'Принять приглашение' }));
    await waitFor(() => expect(push).toHaveBeenCalledWith('/chat'));
  });

  it('renders gone refusal card when preview returns 410', async () => {
    vi.mocked(useAuthStore).mockImplementation(
      (sel: (s: { isAuthenticated: boolean }) => unknown) => sel({ isAuthenticated: true })
    );
    vi.mocked(useInvitationPreview).mockReturnValue({
      isLoading: false,
      isError: true,
      error: { response: { status: 410 } },
      data: undefined,
    } as never);
    vi.mocked(useAcceptInvitation).mockReturnValue({
      mutateAsync: vi.fn(),
      isPending: false,
    } as never);
    render(wrap(<AcceptInvitePage />));
    await waitFor(() =>
      expect(screen.getByText('Ссылка больше не действительна')).toBeInTheDocument()
    );
  });

  it('renders already-member refusal when accept returns 409', async () => {
    vi.mocked(useAuthStore).mockImplementation(
      (sel: (s: { isAuthenticated: boolean }) => unknown) => sel({ isAuthenticated: true })
    );
    vi.mocked(useInvitationPreview).mockReturnValue({
      isLoading: false,
      isError: false,
      data: FRESH_PREVIEW,
    } as never);
    const mutateAsync = vi
      .fn()
      .mockRejectedValue({ response: { status: 409, data: { error: 'already_member' } } });
    vi.mocked(useAcceptInvitation).mockReturnValue({ mutateAsync, isPending: false } as never);
    render(wrap(<AcceptInvitePage />));
    await waitFor(() => screen.getByText('Acme'));
    fireEvent.click(screen.getByRole('button', { name: 'Принять приглашение' }));
    await waitFor(() => expect(screen.getByText('Вы уже в этой организации')).toBeInTheDocument());
  });

  it('fires toast on 500 from accept and stays on the card', async () => {
    vi.mocked(useAuthStore).mockImplementation(
      (sel: (s: { isAuthenticated: boolean }) => unknown) => sel({ isAuthenticated: true })
    );
    vi.mocked(useInvitationPreview).mockReturnValue({
      isLoading: false,
      isError: false,
      data: FRESH_PREVIEW,
    } as never);
    const mutateAsync = vi.fn().mockRejectedValue({ response: { status: 500 } });
    vi.mocked(useAcceptInvitation).mockReturnValue({ mutateAsync, isPending: false } as never);
    render(wrap(<AcceptInvitePage />));
    await waitFor(() => screen.getByText('Acme'));
    fireEvent.click(screen.getByRole('button', { name: 'Принять приглашение' }));
    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith(
        'Не удалось обработать приглашение. Попробуйте ещё раз.'
      )
    );
    expect(screen.getByText('Acme')).toBeInTheDocument();
  });
});
