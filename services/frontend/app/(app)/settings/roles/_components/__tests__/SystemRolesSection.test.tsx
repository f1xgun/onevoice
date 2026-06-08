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

describe('SystemRolesSection', () => {
  it('renders all 4 rows in fixed Owner→Admin→Editor→Viewer order with localized labels', () => {
    renderSection();
    const items = screen.getAllByRole('listitem');
    expect(items[0]).toHaveTextContent('Владелец');
    expect(items[1]).toHaveTextContent('Администратор');
    expect(items[2]).toHaveTextContent('Редактор');
    expect(items[3]).toHaveTextContent('Наблюдатель');
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
    const triggers = screen.getAllByLabelText(/Действия для роли/);
    await user.click(triggers[0]);
    await waitFor(() => {
      expect(screen.getByText('Дублировать')).toBeInTheDocument();
    });
    expect(screen.queryByText('Удалить')).not.toBeInTheDocument();
  });

  it('shows «все права» for owner and «N прав» summary for the other system roles', () => {
    renderSection();
    expect(screen.getByText('все права')).toBeInTheDocument();
    expect(screen.getByText(/10 прав/)).toBeInTheDocument();
  });
});
