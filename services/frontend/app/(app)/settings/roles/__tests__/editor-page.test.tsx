import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

// Mock the data layer BEFORE importing pages so module-init queries
// hit the stub. Both useRoles and the catalog/me-permissions queries
// go through these surfaces — we mock the API, not the hooks.
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

// `useSearchParams` is read by NewRolePage. The mock returns a value that
// the test can mutate before each render via `searchParamsValue` — we cannot
// use `vi.doMock` after the module under test imports next/navigation.
let searchParamsValue: URLSearchParams = new URLSearchParams();
const pushMock = vi.fn();
vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: pushMock }),
  useSearchParams: () => searchParamsValue,
  usePathname: () => '/settings/roles/new',
}));

// Sonner — assert that submit success / failure paths reach toast.*.
const toastSuccess = vi.fn();
const toastError = vi.fn();
vi.mock('sonner', () => ({
  toast: {
    success: (...args: unknown[]) => toastSuccess(...args),
    error: (...args: unknown[]) => toastError(...args),
  },
}));

import { TooltipProvider } from '@/components/ui/tooltip';
import { useBusinessStore } from '@/lib/stores/business';
import { listRoles, createRole, updateRole } from '@/lib/api/roles';
import { getMyPermissions, getPermissionsCatalog } from '@/lib/api/permissions';
import NewRolePage from '../new/page';
import EditRolePage from '../[id]/edit/page';
import { SYSTEM_ROLES, MARKETING_ROLE } from './fixtures/permissions-catalog';

const mockedListRoles = vi.mocked(listRoles);
const mockedCreateRole = vi.mocked(createRole);
const mockedUpdateRole = vi.mocked(updateRole);
const mockedGetMyPerms = vi.mocked(getMyPermissions);
const mockedGetCatalog = vi.mocked(getPermissionsCatalog);

// Full-power actor: holds every permission in MARKETING_ROLE so the clone
// intersection passes through unchanged. The list-page test fixture's
// admin set works as-is.
const FULL_ACTOR_PERMS = [
  'business.read',
  'business.update',
  'roles.read',
  'roles.create',
  'roles.update',
  'roles.delete',
  'members.read',
  'members.invite',
  'members.remove',
  'members.update_role',
];

function renderPage(node: ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={qc}>
        <TooltipProvider delayDuration={0}>{children}</TooltipProvider>
      </QueryClientProvider>
    );
  }
  return render(<Wrapper>{node}</Wrapper>);
}

beforeEach(() => {
  searchParamsValue = new URLSearchParams();
  mockedListRoles.mockReset();
  mockedCreateRole.mockReset();
  mockedUpdateRole.mockReset();
  mockedGetMyPerms.mockReset();
  mockedGetCatalog.mockReset();
  pushMock.mockReset();
  toastSuccess.mockReset();
  toastError.mockReset();
  useBusinessStore.setState({ activeBusinessId: 'biz-1' });
  mockedGetMyPerms.mockResolvedValue(FULL_ACTOR_PERMS);
  mockedListRoles.mockResolvedValue([...SYSTEM_ROLES, MARKETING_ROLE]);
  mockedGetCatalog.mockResolvedValue([
    {
      resource: 'business',
      permissions: [
        { name: 'business.read', description: 'd' },
        { name: 'business.update', description: 'd' },
      ],
    },
    {
      resource: 'roles',
      permissions: [
        { name: 'roles.read', description: 'd' },
        { name: 'roles.create', description: 'd' },
        { name: 'roles.update', description: 'd' },
        { name: 'roles.delete', description: 'd' },
      ],
    },
    {
      resource: 'members',
      permissions: [
        { name: 'members.read', description: 'd' },
        { name: 'members.invite', description: 'd' },
      ],
    },
  ]);
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('NewRolePage create flow', () => {
  it('renders empty form when no clone_from query param', async () => {
    renderPage(<NewRolePage />);
    await waitFor(() => {
      expect((screen.getByLabelText(/Название/i) as HTMLInputElement).value).toBe('');
    });
    expect(screen.getByText(/Новая роль/i)).toBeInTheDocument();
  });

  it('Save button is disabled until the form is dirty (create mode)', async () => {
    renderPage(<NewRolePage />);
    await waitFor(() =>
      expect((screen.getByLabelText(/Название/i) as HTMLInputElement).value).toBe('')
    );
    const saveButtons = screen.getAllByRole('button', { name: /Сохранить/i });
    expect(saveButtons[0]).toBeDisabled();
  });

  it('submits POST /roles and navigates on success', async () => {
    mockedCreateRole.mockResolvedValue({ ...MARKETING_ROLE, member_count: 0 });
    renderPage(<NewRolePage />);
    await waitFor(() => expect(screen.getByLabelText(/Название/i)).toBeInTheDocument());
    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/Название/i), 'Marketing');
    await user.click(screen.getByRole('button', { name: /Сохранить/i }));
    await waitFor(() => expect(mockedCreateRole).toHaveBeenCalled());
    await waitFor(() => expect(pushMock).toHaveBeenCalledWith('/settings/roles'));
    expect(toastSuccess).toHaveBeenCalled();
  });

  it('clone mode pre-fills «Копия — {sourceName}» when ?clone_from=<id>', async () => {
    searchParamsValue = new URLSearchParams(`clone_from=${MARKETING_ROLE.id}`);
    renderPage(<NewRolePage />);
    await waitFor(() => {
      const nameInput = screen.getByLabelText(/Название/i) as HTMLInputElement;
      expect(nameInput.value).toContain('Копия');
      expect(nameInput.value).toContain('Marketing');
    });
  });

  it('shows toast.error and keeps form open on backend failure', async () => {
    const err = {
      isAxiosError: true,
      response: { status: 403, data: { error: 'cannot_grant_unowned_permissions' } },
    };
    mockedCreateRole.mockRejectedValue(err);
    renderPage(<NewRolePage />);
    await waitFor(() => expect(screen.getByLabelText(/Название/i)).toBeInTheDocument());
    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/Название/i), 'BadRole');
    await user.click(screen.getByRole('button', { name: /Сохранить/i }));
    await waitFor(() => expect(toastError).toHaveBeenCalled());
    expect(screen.getByLabelText(/Название/i)).toBeInTheDocument();
    expect(pushMock).not.toHaveBeenCalled();
  });
});

describe('EditRolePage edit flow', () => {
  it('pre-fills form from roles cache (name + description)', async () => {
    renderPage(<EditRolePage params={Promise.resolve({ id: MARKETING_ROLE.id })} />);
    await waitFor(() => {
      expect((screen.getByLabelText(/Название/i) as HTMLInputElement).value).toBe('Marketing');
    });
    expect((screen.getByLabelText(/Описание/i) as HTMLTextAreaElement).value).toBe(
      MARKETING_ROLE.description
    );
  });

  it('Save button stays disabled until the user edits a field', async () => {
    renderPage(<EditRolePage params={Promise.resolve({ id: MARKETING_ROLE.id })} />);
    await waitFor(() =>
      expect((screen.getByLabelText(/Название/i) as HTMLInputElement).value).toBe('Marketing')
    );
    const saveBtn = screen.getAllByRole('button', { name: /Сохранить/i })[0];
    expect(saveBtn).toBeDisabled();
  });

  it('submits PATCH /roles/:id when user edits and saves', async () => {
    mockedUpdateRole.mockResolvedValue({ ...MARKETING_ROLE, name: 'Marketing 2' });
    renderPage(<EditRolePage params={Promise.resolve({ id: MARKETING_ROLE.id })} />);
    await waitFor(() =>
      expect((screen.getByLabelText(/Название/i) as HTMLInputElement).value).toBe('Marketing')
    );
    const user = userEvent.setup();
    const nameInput = screen.getByLabelText(/Название/i);
    await user.clear(nameInput);
    await user.type(nameInput, 'Marketing 2');
    await user.click(screen.getAllByRole('button', { name: /Сохранить/i })[0]);
    await waitFor(() => expect(mockedUpdateRole).toHaveBeenCalled());
    expect(mockedUpdateRole.mock.calls[0]?.[1]).toBe(MARKETING_ROLE.id);
    await waitFor(() => expect(pushMock).toHaveBeenCalledWith('/settings/roles'));
  });
});
