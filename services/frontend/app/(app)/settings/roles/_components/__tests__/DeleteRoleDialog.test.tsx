import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

vi.mock('@/lib/api/roles', () => ({
  listRoles: vi.fn(),
  createRole: vi.fn(),
  updateRole: vi.fn(),
  deleteRole: vi.fn(),
}));
vi.mock('@/lib/api/permissions', () => ({
  getMyPermissions: vi.fn(),
  getPermissionsCatalog: vi.fn(),
}));
vi.mock('sonner', () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
  },
}));

import { TooltipProvider } from '@/components/ui/tooltip';
import { deleteRole } from '@/lib/api/roles';
import { getMyPermissions } from '@/lib/api/permissions';
import { toast } from 'sonner';
import { DeleteRoleDialog, buildReassignOptions } from '../DeleteRoleDialog';
import {
  SYSTEM_ROLES,
  MARKETING_ROLE,
  EMPTY_CUSTOM_ROLE,
  ACTOR_ADMIN_PERMS,
} from '../../__tests__/fixtures/permissions-catalog';

const mockedDeleteRole = vi.mocked(deleteRole);
const mockedGetMyPerms = vi.mocked(getMyPermissions);

function renderDialog(props: Partial<React.ComponentProps<typeof DeleteRoleDialog>> = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={qc}>
        <TooltipProvider delayDuration={0}>{children}</TooltipProvider>
      </QueryClientProvider>
    );
  }
  const onOpenChange = vi.fn();
  return {
    qc,
    onOpenChange,
    ...render(
      <Wrapper>
        <DeleteRoleDialog
          role={EMPTY_CUSTOM_ROLE}
          businessId="biz-1"
          allRoles={[...SYSTEM_ROLES, MARKETING_ROLE, EMPTY_CUSTOM_ROLE]}
          open
          onOpenChange={onOpenChange}
          {...props}
        />
      </Wrapper>
    ),
  };
}

beforeEach(() => {
  mockedDeleteRole.mockReset();
  mockedGetMyPerms.mockReset();
  vi.mocked(toast.success).mockReset();
  vi.mocked(toast.error).mockReset();
  // Default actor: full admin perms (can grant every system role's perm set).
  mockedGetMyPerms.mockResolvedValue([...ACTOR_ADMIN_PERMS]);
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('DeleteRoleDialog — D-08 smart variant branching', () => {
  it('simple variant when member_count=0 → DELETE without reassign_to', async () => {
    mockedDeleteRole.mockResolvedValue(undefined);
    renderDialog({ role: EMPTY_CUSTOM_ROLE });
    // Simple body copy.
    expect(screen.getByText('Действие нельзя отменить.')).toBeInTheDocument();
    // No Select rendered in simple variant.
    expect(screen.queryByText('Перенести участников на:')).not.toBeInTheDocument();
    // Click confirm.
    const confirm = screen.getByRole('button', { name: 'Удалить' });
    await userEvent.setup().click(confirm);
    await waitFor(() => {
      expect(mockedDeleteRole).toHaveBeenCalledWith('biz-1', EMPTY_CUSTOM_ROLE.id, null);
    });
  });

  it('picker variant when member_count>0 → renders Select + member-count body', () => {
    renderDialog({ role: MARKETING_ROLE });
    expect(screen.getByText(/На эту роль назначены участники \(5\)/)).toBeInTheDocument();
    expect(screen.getByText('Перенести участников на:')).toBeInTheDocument();
  });

  it('picker variant disables confirm until a reassign target is auto-selected', async () => {
    mockedDeleteRole.mockResolvedValue(undefined);
    renderDialog({ role: MARKETING_ROLE });
    // useEffect auto-selects the first eligible option once actor perms load.
    await waitFor(() => {
      const confirm = screen.getByRole('button', { name: 'Удалить' });
      expect(confirm).not.toBeDisabled();
    });
  });
});

describe('DeleteRoleDialog — race recovery', () => {
  it('on simple-variant 422 role_in_use → flips to picker in-place (dialog stays open)', async () => {
    const err = {
      isAxiosError: true,
      response: { status: 422, data: { error: 'role_in_use' } },
    };
    mockedDeleteRole.mockRejectedValueOnce(err);
    const { onOpenChange } = renderDialog({ role: EMPTY_CUSTOM_ROLE });
    await userEvent.setup().click(screen.getByRole('button', { name: 'Удалить' }));
    // After the race-recovery branch fires, the dialog body switches from
    // simpleBody → pickerBody WITHOUT closing.
    await waitFor(() => {
      expect(screen.queryByText('Действие нельзя отменить.')).not.toBeInTheDocument();
      expect(screen.getByText('Перенести участников на:')).toBeInTheDocument();
    });
    // Dialog MUST NOT have been closed by the failed simple attempt.
    expect(onOpenChange).not.toHaveBeenCalledWith(false);
    // No error toast — race recovery is silent.
    expect(toast.error).not.toHaveBeenCalled();
  });

  it('on success → closes the dialog and shows success toast', async () => {
    mockedDeleteRole.mockResolvedValue(undefined);
    const { onOpenChange } = renderDialog({ role: EMPTY_CUSTOM_ROLE });
    await userEvent.setup().click(screen.getByRole('button', { name: 'Удалить' }));
    await waitFor(() => {
      expect(onOpenChange).toHaveBeenCalledWith(false);
      expect(toast.success).toHaveBeenCalledWith('Роль удалена');
    });
  });

  it('on unrelated error (500) → shows toast.error via mapRoleError, dialog stays open', async () => {
    mockedDeleteRole.mockRejectedValueOnce({
      isAxiosError: true,
      response: { status: 500, data: { error: 'internal' } },
    });
    const { onOpenChange } = renderDialog({ role: EMPTY_CUSTOM_ROLE });
    await userEvent.setup().click(screen.getByRole('button', { name: 'Удалить' }));
    await waitFor(() => {
      expect(toast.error).toHaveBeenCalled();
    });
    expect(onOpenChange).not.toHaveBeenCalledWith(false);
  });
});

describe('buildReassignOptions (D-09 ordering)', () => {
  it('orders system roles Owner→Admin→Editor→Viewer, then custom A→Z', () => {
    const opts = buildReassignOptions({
      allRoles: [...SYSTEM_ROLES, MARKETING_ROLE, EMPTY_CUSTOM_ROLE],
      excludeRoleId: 'no-match',
      actorPerms: new Set([
        'business.read',
        'business.update',
        'business.delete',
        'business.transfer_ownership',
        'roles.read',
        'roles.create',
        'roles.update',
        'roles.delete',
        'members.read',
        'members.invite',
        'members.remove',
        'members.update_role',
      ]),
    });
    const labels = opts.map((o) => o.label);
    expect(labels[0]).toBe('Владелец');
    expect(labels[1]).toBe('Администратор');
    expect(labels[2]).toBe('Редактор');
    expect(labels[3]).toBe('Наблюдатель');
    // Custom comes after — alphabetical (Empty Role < Marketing in 'ru').
    expect(labels[4]).toBe('Empty Role');
    expect(labels[5]).toBe('Marketing');
  });

  it('disables options the actor cannot grant (escalation guard)', () => {
    const opts = buildReassignOptions({
      allRoles: SYSTEM_ROLES,
      excludeRoleId: 'no-match',
      actorPerms: new Set(['business.read']), // weak actor
    });
    // Owner enumerates every permission — actor with one perm cannot grant.
    expect(opts.find((o) => o.label === 'Владелец')?.disabled).toBe(true);
  });

  it('excludes the role being deleted (self-exclusion)', () => {
    const opts = buildReassignOptions({
      allRoles: SYSTEM_ROLES,
      excludeRoleId: SYSTEM_ROLES[0].id, // owner
      actorPerms: new Set([
        'business.read',
        'business.update',
        'business.delete',
        'business.transfer_ownership',
        'roles.read',
        'roles.create',
        'roles.update',
        'roles.delete',
        'members.read',
        'members.invite',
        'members.remove',
        'members.update_role',
      ]),
    });
    expect(opts.find((o) => o.label === 'Владелец')).toBeUndefined();
  });
});
