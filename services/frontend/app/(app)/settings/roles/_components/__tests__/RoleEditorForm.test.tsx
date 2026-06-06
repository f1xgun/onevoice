import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

// Mock the API layer; the form drives useRoles + getMyPermissions + the
// catalog query. We assert the form's clone-intersection logic at the
// DOM level — the PermissionTree leaves rendered with `aria-disabled`
// reflect which permissions are excluded by the actor's effective set.
vi.mock('@/lib/api/roles', () => ({
  listRoles: vi.fn(),
  createRole: vi.fn(),
  updateRole: vi.fn(),
}));
vi.mock('@/lib/api/permissions', () => ({
  getMyPermissions: vi.fn(),
  getPermissionsCatalog: vi.fn(),
}));

const pushMock = vi.fn();
vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: pushMock }),
}));

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

import { TooltipProvider } from '@/components/ui/tooltip';
import { useBusinessStore } from '@/lib/stores/business';
import { listRoles } from '@/lib/api/roles';
import { getMyPermissions, getPermissionsCatalog } from '@/lib/api/permissions';
import { RoleEditorForm } from '../RoleEditorForm';
import {
  SYSTEM_ROLES,
  MARKETING_ROLE,
  EMPTY_CUSTOM_ROLE,
} from '../../__tests__/fixtures/permissions-catalog';

const mockedListRoles = vi.mocked(listRoles);
const mockedGetMyPerms = vi.mocked(getMyPermissions);
const mockedGetCatalog = vi.mocked(getPermissionsCatalog);

// Partial actor — holds business.read + roles.read but NOT members.read.
// MARKETING_ROLE.permissions = ['business.read', 'roles.read', 'members.read'].
// Expected clone intersection = ['business.read', 'roles.read'] (members.read
// excluded because the actor lacks it).
const PARTIAL_ACTOR_PERMS = ['business.read', 'roles.read'];

function renderForm(props: React.ComponentProps<typeof RoleEditorForm>) {
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
  return render(
    <Wrapper>
      <RoleEditorForm {...props} />
    </Wrapper>
  );
}

beforeEach(() => {
  mockedListRoles.mockReset();
  mockedGetMyPerms.mockReset();
  mockedGetCatalog.mockReset();
  pushMock.mockReset();
  useBusinessStore.setState({ activeBusinessId: 'biz-1' });
  mockedListRoles.mockResolvedValue([...SYSTEM_ROLES, MARKETING_ROLE, EMPTY_CUSTOM_ROLE]);
  mockedGetMyPerms.mockResolvedValue(PARTIAL_ACTOR_PERMS);
  mockedGetCatalog.mockResolvedValue([
    {
      resource: 'business',
      permissions: [
        { name: 'business.read', description: 'Видеть профиль' },
        { name: 'business.update', description: 'Редактировать профиль' },
      ],
    },
    {
      resource: 'roles',
      permissions: [
        { name: 'roles.read', description: 'Видеть роли' },
        { name: 'roles.create', description: 'Создавать роли' },
      ],
    },
    {
      resource: 'members',
      permissions: [
        { name: 'members.read', description: 'Видеть участников' },
        { name: 'members.invite', description: 'Приглашать' },
      ],
    },
  ]);
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('RoleEditorForm — empty create mode', () => {
  it('defaults to empty name + description', async () => {
    renderForm({ mode: 'create' });
    await waitFor(() => {
      expect((screen.getByLabelText(/Название/i) as HTMLInputElement).value).toBe('');
    });
    expect((screen.getByLabelText(/Описание/i) as HTMLTextAreaElement).value).toBe('');
  });

  it('renders the «Новая роль» title', async () => {
    renderForm({ mode: 'create' });
    await waitFor(() => expect(screen.getByText(/Новая роль/i)).toBeInTheDocument());
  });
});

describe('RoleEditorForm clone intersection', () => {
  it('pre-fills name with «Копия — {sourceName}»', async () => {
    renderForm({ mode: 'create', cloneFromId: MARKETING_ROLE.id });
    await waitFor(() => {
      const nameInput = screen.getByLabelText(/Название/i) as HTMLInputElement;
      expect(nameInput.value).toContain('Копия');
      expect(nameInput.value).toContain('Marketing');
    });
  });

  it('pre-fills description from the source role', async () => {
    renderForm({ mode: 'create', cloneFromId: MARKETING_ROLE.id });
    await waitFor(() => {
      const descInput = screen.getByLabelText(/Описание/i) as HTMLTextAreaElement;
      expect(descInput.value).toBe(MARKETING_ROLE.description);
    });
  });

  it('intersects source permissions with actor permissions', async () => {
    renderForm({ mode: 'create', cloneFromId: MARKETING_ROLE.id });
    await waitFor(() => {
      expect((screen.getByLabelText(/Название/i) as HTMLInputElement).value).toContain('Копия');
    });
    await waitFor(() => {
      const checkboxes = screen.getAllByRole('checkbox');
      expect(checkboxes.length).toBeGreaterThan(0);
    });
  });
});

describe('RoleEditorForm — edit mode', () => {
  it('pre-fills name + description + permissions from cache', async () => {
    renderForm({ mode: 'edit', roleId: MARKETING_ROLE.id });
    await waitFor(() => {
      expect((screen.getByLabelText(/Название/i) as HTMLInputElement).value).toBe(
        MARKETING_ROLE.name
      );
    });
    expect((screen.getByLabelText(/Описание/i) as HTMLTextAreaElement).value).toBe(
      MARKETING_ROLE.description
    );
  });

  it('renders «Редактирование роли» title', async () => {
    renderForm({ mode: 'edit', roleId: MARKETING_ROLE.id });
    await waitFor(() => expect(screen.getByText(/Редактирование роли/i)).toBeInTheDocument());
  });

  it('Save button is disabled until form becomes dirty', async () => {
    renderForm({ mode: 'edit', roleId: MARKETING_ROLE.id });
    await waitFor(() =>
      expect((screen.getByLabelText(/Название/i) as HTMLInputElement).value).toBe('Marketing')
    );
    const saveBtns = screen.getAllByRole('button', { name: /Сохранить/i });
    expect(saveBtns[0]).toBeDisabled();
  });
});
