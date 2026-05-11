import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { NextIntlClientProvider } from 'next-intl';
import type { ReactNode } from 'react';
import messages from '@/messages/ru.json';

vi.mock('@/lib/stores/business', () => ({ useBusinessStore: vi.fn() }));
vi.mock('@/lib/hooks/useBusinessList', () => ({
  useBusinessList: vi.fn(),
  BUSINESS_LIST_QUERY_KEY: ['businesses'],
}));
vi.mock('@/lib/hooks/useMembers', () => ({
  useMembers: vi.fn(),
  useRoles: vi.fn(),
  useUpdateMemberRole: vi.fn(),
  useRemoveMember: vi.fn(),
}));
vi.mock('@/lib/hooks/useInvitations', () => ({
  useInvitations: vi.fn(),
  useCreateInvitation: vi.fn(),
  useRevokeInvitation: vi.fn(),
}));
vi.mock('@/lib/auth', () => ({ useAuthStore: vi.fn() }));
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() } }));

import { useBusinessStore } from '@/lib/stores/business';
import { useBusinessList } from '@/lib/hooks/useBusinessList';
import { useMembers, useRoles, useUpdateMemberRole, useRemoveMember } from '@/lib/hooks/useMembers';
import {
  useInvitations,
  useCreateInvitation,
  useRevokeInvitation,
} from '@/lib/hooks/useInvitations';
import { useAuthStore } from '@/lib/auth';
import { toast } from 'sonner';
import TeamPage from '../page';

/* eslint-disable @typescript-eslint/no-explicit-any */

function wrap(node: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    <NextIntlClientProvider locale="ru" messages={messages}>
      <QueryClientProvider client={qc}>{node}</QueryClientProvider>
    </NextIntlClientProvider>
  );
}

const ADMIN_BUSINESS = {
  id: 'biz-1',
  name: 'Acme',
  role: { id: 'role-admin', name: 'admin' },
  status: 'active' as const,
  joined_at: '2026-05-01T00:00:00Z',
};

const SAMPLE_ROLES = [
  { id: 'role-viewer', business_id: null, name: 'viewer', permissions: [], is_system: true },
  { id: 'role-admin', business_id: null, name: 'admin', permissions: [], is_system: true },
];

const SAMPLE_MEMBERS = [
  {
    user: { id: 'u-1', email: 'me@example.com', name: 'Me' },
    role: { id: 'role-admin', name: 'admin', permissions: [] },
    status: 'active' as const,
    joined_at: '2026-05-01T00:00:00Z',
    invited_by: null,
    invited_at: null,
  },
  {
    user: { id: 'u-2', email: 'other@example.com', name: 'Other' },
    role: { id: 'role-viewer', name: 'viewer', permissions: [] },
    status: 'active' as const,
    joined_at: '2026-05-02T00:00:00Z',
    invited_by: null,
    invited_at: null,
  },
];

beforeEach(() => {
  vi.mocked(useBusinessStore).mockImplementation((sel: any) =>
    sel({ activeBusinessId: 'biz-1', setActive: vi.fn(), clear: vi.fn() })
  );
  vi.mocked(useBusinessList).mockReturnValue({
    data: [ADMIN_BUSINESS],
    isLoading: false,
  } as any);
  vi.mocked(useMembers).mockReturnValue({
    data: SAMPLE_MEMBERS,
    isLoading: false,
    isError: false,
  } as any);
  vi.mocked(useRoles).mockReturnValue({ data: SAMPLE_ROLES, isLoading: false } as any);
  vi.mocked(useInvitations).mockReturnValue({ data: [], isLoading: false } as any);
  vi.mocked(useCreateInvitation).mockReturnValue({
    mutateAsync: vi.fn(),
    isPending: false,
  } as any);
  vi.mocked(useUpdateMemberRole).mockReturnValue({
    mutateAsync: vi.fn(),
    isPending: false,
  } as any);
  vi.mocked(useRemoveMember).mockReturnValue({
    mutateAsync: vi.fn(),
    isPending: false,
  } as any);
  vi.mocked(useRevokeInvitation).mockReturnValue({
    mutateAsync: vi.fn(),
    isPending: false,
  } as any);
  vi.mocked(useAuthStore).mockImplementation((sel: any) =>
    sel({ user: { id: 'u-1', email: 'me@example.com' } })
  );
  vi.mocked(toast.error).mockReset();
});

describe('TeamPage', () => {
  it('renders the page header + tabs', () => {
    render(wrap(<TeamPage />));
    expect(screen.getAllByText('Команда').length).toBeGreaterThan(0);
    expect(screen.getByRole('tab', { name: /Участники/ })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: /Приглашения/ })).toBeInTheDocument();
  });

  it('shows the «Пригласить» button when user has members.invite (admin role)', () => {
    render(wrap(<TeamPage />));
    expect(screen.getByRole('button', { name: /Пригласить/ })).toBeInTheDocument();
  });

  it('renders members rows with name, email, role pill', () => {
    render(wrap(<TeamPage />));
    expect(screen.getByText('Me')).toBeInTheDocument();
    expect(screen.getByText('other@example.com')).toBeInTheDocument();
  });

  it('shows «Покинуть организацию» for the current user row (self-row)', async () => {
    const user = userEvent.setup();
    render(wrap(<TeamPage />));
    const triggers = screen.getAllByLabelText(/Действия для участника/);
    await user.click(triggers[0]);
    expect(await screen.findByText('Покинуть организацию')).toBeInTheDocument();
  });

  it('fires the last_owner toast on 422 remove', async () => {
    const err = { response: { status: 422, data: { error: 'last_owner' } } };
    vi.mocked(useRemoveMember).mockReturnValue({
      mutateAsync: vi.fn().mockRejectedValue(err),
      isPending: false,
    } as any);
    const user = userEvent.setup();
    render(wrap(<TeamPage />));
    const triggers = screen.getAllByLabelText(/Действия для участника/);
    await user.click(triggers[1]);
    await user.click(await screen.findByText('Удалить участника'));
    await user.click(await screen.findByRole('button', { name: 'Удалить' }));
    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith(
        'Нельзя удалить последнего владельца. Сначала назначьте нового владельца.'
      )
    );
  });
});
