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
const setAuth = vi.fn();
const setAccessToken = vi.fn();
vi.mock('@/lib/auth', () => {
  const useAuthStore = vi.fn();
  (useAuthStore as unknown as { getState: () => unknown }).getState = () => ({
    isAuthenticated: authedAtMount,
    setAuth,
    setAccessToken,
  });
  return { useAuthStore };
});
vi.mock('@/lib/api', () => ({ api: { post: vi.fn() } }));
// hardNav records every next-less hard navigation to a bare '/login'. The
// regressed bootstrap path calls refreshAccessToken(); the real function does
// `window.location.href = '/login'` on a session-terminal 401, which STRIPS
// the ?next= deep link. We mock refreshAccessToken to reproduce that exact
// side effect (reject AND fire the bare-'/login' hard nav), so reverting the
// page from the raw `api.post('/auth/refresh')` back to refreshAccessToken()
// makes the anonymous next-preservation assertions fail. Under the merged fix
// the page never touches refreshAccessToken, so the mock stays untouched.
const hardNav = vi.fn();
const refreshAccessToken = vi.fn();
vi.mock('@/lib/api/authFetch', () => ({
  refreshAccessToken: (...args: unknown[]) => refreshAccessToken(...args),
}));
vi.mock('@/lib/hooks/useInvitations', () => ({
  useInvitationPreview: vi.fn(),
  useAcceptInvitation: vi.fn(),
}));
vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: vi.fn(),
}));
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() } }));

import { useAuthStore } from '@/lib/auth';
import { api } from '@/lib/api';
import { useInvitationPreview, useAcceptInvitation } from '@/lib/hooks/useInvitations';
import { useBusinessStore } from '@/lib/stores/business';
import { toast } from 'sonner';
import AcceptInvitePage from '../../../app/(public)/invite/[token]/page';

// Mirrors the in-memory store at mount: the page reads
// useAuthStore.getState().isAuthenticated synchronously in its bootstrap
// effect before deciding whether to attempt a refresh.
let authedAtMount = false;

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

function mockAuth(isAuthenticated: boolean) {
  authedAtMount = isAuthenticated;
  vi.mocked(useAuthStore).mockImplementation((sel: (s: { isAuthenticated: boolean }) => unknown) =>
    sel({ isAuthenticated })
  );
}

beforeEach(() => {
  replace.mockReset();
  push.mockReset();
  authedAtMount = false;
  setAuth.mockReset();
  setAccessToken.mockReset();
  vi.mocked(api.post).mockReset();
  hardNav.mockReset();
  // Faithful stand-in for the real refreshAccessToken on a session-terminal
  // 401: it rejects AND hard-navigates to a bare '/login', dropping ?next=.
  // The merged page never calls this; a regressed page that calls it instead
  // of `api.post('/auth/refresh')` will trip the next-preservation guards.
  refreshAccessToken.mockReset();
  refreshAccessToken.mockImplementation(() => {
    hardNav('/login');
    return Promise.reject({ response: { status: 401 } });
  });
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
  it('redirects anonymous users to /login?next=/invite/{token} preserving next via raw refresh (no next-less hard nav)', async () => {
    mockAuth(false);
    // Anonymous visitor: the cold-load bootstrap POSTs /auth/refresh -> 401.
    // The merged page does this through the raw, NON-redirecting api.post
    // (lib/api classifies /auth/refresh as an auth endpoint, so its 401
    // interceptor rejects LOCALLY) and then owns the redirect itself,
    // preserving ?next=. We mock api.post to reject 401 so that path runs.
    vi.mocked(api.post).mockRejectedValue({ response: { status: 401 } });
    vi.mocked(useInvitationPreview).mockReturnValue({ isLoading: true } as never);
    vi.mocked(useAcceptInvitation).mockReturnValue({
      mutateAsync: vi.fn(),
      isPending: false,
    } as never);
    render(wrap(<AcceptInvitePage />));
    await waitFor(() => expect(replace).toHaveBeenCalledWith('/login?next=/invite/abc123'));
    // FAIL-ON-REVERT guards. The deep link must survive the redirect:
    //  1. The page must NOT route through refreshAccessToken — that function
    //     hard-navigates to a bare '/login' on a 401, STRIPPING ?next=. The
    //     mock (beforeEach) fires `hardNav('/login')` if called, so any call
    //     trips both guards below. The merged fix leaves it untouched.
    //  2. Every router.replace must carry ?next= back to the invite.
    // Reverting the page from `api.post('/auth/refresh')` to
    // `refreshAccessToken()` makes (a) refreshAccessToken get called and
    // (b) hardNav fire to a next-less '/login', failing this test.
    expect(refreshAccessToken).not.toHaveBeenCalled();
    expect(hardNav).not.toHaveBeenCalled();
    for (const call of replace.mock.calls) {
      expect(call).toEqual(['/login?next=/invite/abc123']);
    }
  });

  it('bootstraps the session from the refresh cookie and renders the authed view without redirecting', async () => {
    mockAuth(false);
    // Already-logged-in visitor with only the httpOnly refresh cookie: the
    // raw /auth/refresh resolves a fresh session. The page must set the token
    // and render the invite view — no redirect to /login.
    vi.mocked(api.post).mockResolvedValue({ data: { accessToken: 'new-access-token' } } as never);
    vi.mocked(useInvitationPreview).mockReturnValue({
      isLoading: false,
      isError: false,
      data: FRESH_PREVIEW,
    } as never);
    vi.mocked(useAcceptInvitation).mockReturnValue({
      mutateAsync: vi.fn(),
      isPending: false,
    } as never);
    const { rerender } = render(wrap(<AcceptInvitePage />));
    await waitFor(() => expect(setAccessToken).toHaveBeenCalledWith('new-access-token'));
    mockAuth(true);
    rerender(wrap(<AcceptInvitePage />));
    await waitFor(() => expect(screen.getByText('Acme')).toBeInTheDocument());
    expect(replace).not.toHaveBeenCalled();
  });

  it('renders the preview card for authenticated users', async () => {
    mockAuth(true);
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
    mockAuth(true);
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
    mockAuth(true);
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
    mockAuth(true);
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
    mockAuth(true);
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
