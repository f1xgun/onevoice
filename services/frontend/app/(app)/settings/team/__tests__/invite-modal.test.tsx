import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { NextIntlClientProvider } from 'next-intl';
import type { ReactNode } from 'react';
import messages from '@/messages/ru.json';

vi.mock('@/lib/hooks/useInvitations', () => ({
  useCreateInvitation: vi.fn(),
}));
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() } }));

import { useCreateInvitation } from '@/lib/hooks/useInvitations';
import { toast } from 'sonner';
import { InviteModal } from '../_components/InviteModal';

const mockedCreate = vi.mocked(useCreateInvitation);

function wrap(node: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    <NextIntlClientProvider locale="ru" messages={messages}>
      <QueryClientProvider client={qc}>{node}</QueryClientProvider>
    </NextIntlClientProvider>
  );
}

const SAMPLE_ROLES = [
  { id: 'role-owner', business_id: null, name: 'owner', permissions: [], is_system: true },
  { id: 'role-admin', business_id: null, name: 'admin', permissions: [], is_system: true },
  { id: 'role-viewer', business_id: null, name: 'viewer', permissions: [], is_system: true },
];

beforeEach(() => {
  mockedCreate.mockReset();
  vi.mocked(toast.success).mockReset();
  Object.defineProperty(navigator, 'clipboard', {
    value: { writeText: vi.fn().mockResolvedValue(undefined) },
    writable: true,
    configurable: true,
  });
});

describe('InviteModal', () => {
  it('renders the form (State A) with role + expiry selects', () => {
    mockedCreate.mockReturnValue({
      mutateAsync: vi.fn(),
      isPending: false,
    } as unknown as ReturnType<typeof useCreateInvitation>);
    render(
      wrap(
        <InviteModal
          open
          onOpenChange={vi.fn()}
          businessId="biz-1"
          roles={SAMPLE_ROLES}
          onInvitationCreated={vi.fn()}
        />
      )
    );
    expect(screen.getByText('Пригласить участника')).toBeInTheDocument();
    for (const select of screen.getAllByRole('combobox')) expect(select).toHaveAccessibleName();
    expect(screen.getByRole('button', { name: /Создать ссылку/ })).toBeInTheDocument();
  });

  it('on successful submit, swaps to copy state and writes to clipboard', async () => {
    const onCreated = vi.fn();
    mockedCreate.mockReturnValue({
      mutateAsync: vi.fn().mockResolvedValue({
        id: 'inv-1',
        token: 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA',
        role_id: 'role-viewer',
        expires_at: '2026-05-17T00:00:00Z',
        created_at: '2026-05-10T00:00:00Z',
      }),
      isPending: false,
    } as unknown as ReturnType<typeof useCreateInvitation>);

    render(
      wrap(
        <InviteModal
          open
          onOpenChange={vi.fn()}
          businessId="biz-1"
          roles={SAMPLE_ROLES}
          onInvitationCreated={onCreated}
        />
      )
    );

    const form = screen.getByRole('button', { name: /Создать ссылку/ }).closest('form');
    expect(form).not.toBeNull();
    fireEvent.submit(form!);

    await waitFor(() => expect(screen.getByText('Ссылка готова')).toBeInTheDocument());
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith(
      expect.stringContaining('/invite/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA')
    );
    expect(toast.success).toHaveBeenCalledWith('Ссылка скопирована');
    expect(onCreated).toHaveBeenCalledWith('inv-1', expect.stringContaining('A'));
  });

  it('on 429 too_many_pending, renders inline error and stays on form state', async () => {
    const err = { response: { status: 429, data: { error: 'too_many_pending' } } };
    mockedCreate.mockReturnValue({
      mutateAsync: vi.fn().mockRejectedValue(err),
      isPending: false,
    } as unknown as ReturnType<typeof useCreateInvitation>);

    render(
      wrap(
        <InviteModal
          open
          onOpenChange={vi.fn()}
          businessId="biz-1"
          roles={SAMPLE_ROLES}
          onInvitationCreated={vi.fn()}
        />
      )
    );

    const form = screen.getByRole('button', { name: /Создать ссылку/ }).closest('form');
    expect(form).not.toBeNull();
    fireEvent.submit(form!);

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('Достигнут лимит'));
    expect(screen.queryByText('Ссылка готова')).not.toBeInTheDocument();
  });
});
