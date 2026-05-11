import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

// Mock the data layer BEFORE importing the page so module-init queries
// hit the stub. Plan 05-04's useRoles wraps listRoles via React Query —
// we mock the API surface, not the hook itself, so loading/error
// branches are exercised end-to-end.
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

const pushMock = vi.fn();
vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: pushMock }),
  usePathname: () => '/settings/roles',
}));

import { TooltipProvider } from '@/components/ui/tooltip';
import { useBusinessStore } from '@/lib/stores/business';
import { listRoles } from '@/lib/api/roles';
import { getMyPermissions } from '@/lib/api/permissions';
import RolesPage from '../page';
import { SYSTEM_ROLES, MARKETING_ROLE, EMPTY_CUSTOM_ROLE } from './fixtures/permissions-catalog';

const mockedListRoles = vi.mocked(listRoles);
const mockedGetMyPerms = vi.mocked(getMyPermissions);

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={qc}>
        <TooltipProvider delayDuration={0}>{children}</TooltipProvider>
      </QueryClientProvider>
    );
  }
  return render(
    <Wrapper>
      <RolesPage />
    </Wrapper>
  );
}

beforeEach(() => {
  mockedListRoles.mockReset();
  mockedGetMyPerms.mockReset();
  pushMock.mockReset();
  // Generous actor — full admin perms; lets the «Новая роль» CTA + the
  // row action menus render so the assertions can find them.
  mockedGetMyPerms.mockResolvedValue([
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
  ]);
  useBusinessStore.setState({ activeBusinessId: 'biz-1' });
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('RolesPage (UI-RBAC-08)', () => {
  it('renders both sections with all system + custom rows', async () => {
    mockedListRoles.mockResolvedValue([...SYSTEM_ROLES, MARKETING_ROLE, EMPTY_CUSTOM_ROLE]);
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Системные роли')).toBeInTheDocument();
      expect(screen.getByText('Свои роли')).toBeInTheDocument();
      expect(screen.getByText('owner')).toBeInTheDocument();
      expect(screen.getByText('Marketing')).toBeInTheDocument();
    });
  });

  it('renders skeletons + page title while loading', async () => {
    // Pending forever — keeps isLoading=true.
    mockedListRoles.mockImplementation(() => new Promise(() => {}));
    renderPage();
    // The page title appears once the RequirePermission gate resolves
    // (permissions resolve in the next microtask via getMyPermissions mock).
    await waitFor(() => {
      expect(screen.getByText('Роли')).toBeInTheDocument();
    });
    // While roles are still loading, the section containers carry aria-busy.
    const busy = document.querySelectorAll('[aria-busy="true"]');
    expect(busy.length).toBeGreaterThan(0);
  });

  it('renders error state with retry button when fetch fails', async () => {
    mockedListRoles.mockRejectedValue(new Error('network'));
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Попробовать снова')).toBeInTheDocument();
    });
  });

  it('shows empty-state copy for custom section when only system roles exist', async () => {
    mockedListRoles.mockResolvedValue([...SYSTEM_ROLES]);
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Пока нет своих ролей')).toBeInTheDocument();
    });
  });
});
