import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

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
import { getMyPermissions } from '@/lib/api/permissions';
import { SystemRolesSection } from '../SystemRolesSection';
import { SYSTEM_ROLES } from '../../__tests__/fixtures/permissions-catalog';

const mockedGetMyPerms = vi.mocked(getMyPermissions);

function renderSection() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={qc}>
        <TooltipProvider delayDuration={0}>{children}</TooltipProvider>
      </QueryClientProvider>
    );
  }
  // Pass roles intentionally OUT OF ORDER so the test can assert that the
  // component reorders them by SYSTEM_ORDER (Owner→Admin→Editor→Viewer).
  const scrambled = [
    SYSTEM_ROLES[3], // viewer
    SYSTEM_ROLES[2], // editor
    SYSTEM_ROLES[0], // owner
    SYSTEM_ROLES[1], // admin
  ];
  return render(
    <Wrapper>
      <SystemRolesSection roles={scrambled} businessId="biz-1" />
    </Wrapper>
  );
}

beforeEach(() => {
  mockedGetMyPerms.mockReset();
  pushMock.mockReset();
  // Full admin perms so the menu's «Дублировать» action surfaces.
  mockedGetMyPerms.mockResolvedValue([
    'roles.read',
    'roles.create',
    'roles.update',
    'roles.delete',
  ]);
  useBusinessStore.setState({ activeBusinessId: 'biz-1' });
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('SystemRolesSection (D-02 / D-03)', () => {
  it('renders all 4 rows in fixed Owner→Admin→Editor→Viewer order regardless of input order', () => {
    renderSection();
    const items = screen.getAllByRole('listitem');
    expect(items[0]).toHaveTextContent('owner');
    expect(items[1]).toHaveTextContent('admin');
    expect(items[2]).toHaveTextContent('editor');
    expect(items[3]).toHaveTextContent('viewer');
  });

  it('applies opacity-60 to make system rows visually muted (non-interactive)', () => {
    renderSection();
    const items = screen.getAllByRole('listitem');
    items.forEach((row) => {
      expect(row.className).toContain('opacity-60');
    });
  });

  it('renders «системная» badge on every system row', () => {
    renderSection();
    const badges = screen.getAllByText('системная');
    expect(badges).toHaveLength(4);
  });

  it('action menu shows «Дублировать» but NOT «Удалить» for system rows', async () => {
    renderSection();
    const user = userEvent.setup();
    // Open the menu for the first row (owner).
    const triggers = screen.getAllByLabelText(/Действия для роли/);
    await user.click(triggers[0]);
    await waitFor(() => {
      expect(screen.getByText('Дублировать')).toBeInTheDocument();
    });
    // «Удалить» MUST NOT appear — system roles are immutable.
    expect(screen.queryByText('Удалить')).not.toBeInTheDocument();
  });

  it('shows «все права» for owner and «N прав» summary for the other system roles', () => {
    renderSection();
    // Owner has every perm — should render the all-perms badge.
    expect(screen.getByText('все права')).toBeInTheDocument();
    // Admin in the fixture has 10 perms; expect "10 прав".
    expect(screen.getByText(/10 прав/)).toBeInTheDocument();
  });
});
