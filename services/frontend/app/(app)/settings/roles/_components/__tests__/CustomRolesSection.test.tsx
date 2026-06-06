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
import { CustomRolesSection } from '../CustomRolesSection';
import {
  SYSTEM_ROLES,
  MARKETING_ROLE,
  EMPTY_CUSTOM_ROLE,
  ANALYTICS_ROLE,
} from '../../__tests__/fixtures/permissions-catalog';

const mockedGetMyPerms = vi.mocked(getMyPermissions);

function renderSection(opts?: { roles?: (typeof MARKETING_ROLE)[]; perms?: string[] }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={qc}>
        <TooltipProvider delayDuration={0}>{children}</TooltipProvider>
      </QueryClientProvider>
    );
  }
  if (opts?.perms !== undefined) {
    mockedGetMyPerms.mockResolvedValue(opts.perms);
  }
  const roles = opts?.roles ?? [MARKETING_ROLE, ANALYTICS_ROLE, EMPTY_CUSTOM_ROLE];
  return render(
    <Wrapper>
      <CustomRolesSection roles={roles} businessId="biz-1" allRoles={[...SYSTEM_ROLES, ...roles]} />
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

describe('CustomRolesSection', () => {
  it('sorts custom rows alphabetically A→Z', async () => {
    renderSection();
    await waitFor(() => {
      const items = screen.getAllByRole('link', { name: /Analytics|Empty Role|Marketing/ });
      const rowLinks = items.filter((el) => el.getAttribute('href')?.endsWith('/edit'));
      const texts = rowLinks.map((el) => el.textContent);
      expect(texts[0]).toContain('Analytics');
      expect(texts[1]).toContain('Empty Role');
      expect(texts[2]).toContain('Marketing');
    });
  });

  it('row is rendered as a Link (anchor) to /settings/roles/[id]/edit', () => {
    renderSection({ roles: [MARKETING_ROLE] });
    const link = screen.getByRole('link', { name: /Marketing/ });
    expect(link.tagName.toLowerCase()).toBe('a');
    expect(link.getAttribute('href')).toBe(`/settings/roles/${MARKETING_ROLE.id}/edit`);
  });

  it('renders «N участников» member count column on each custom row', () => {
    renderSection({ roles: [MARKETING_ROLE] });
    expect(screen.getByText(/5 участников/)).toBeInTheDocument();
  });

  it('renders empty-state copy + CTA when there are no custom roles', () => {
    renderSection({ roles: [] });
    expect(screen.getByText('Пока нет своих ролей')).toBeInTheDocument();
    expect(screen.getByText('Создайте первую роль с особым набором прав.')).toBeInTheDocument();
  });

  it('renders «+ Новая роль» CTA gated by roles.create permission (visible when allowed)', async () => {
    renderSection();
    await waitFor(() => {
      const cta = screen.getByRole('link', { name: /Новая роль/ });
      expect(cta.getAttribute('href')).toBe('/settings/roles/new');
    });
  });

  it('hides «+ Новая роль» CTA when actor lacks roles.create', async () => {
    renderSection({ perms: ['roles.read'] });
    await waitFor(() => {
      expect(screen.queryByRole('link', { name: /Новая роль/ })).not.toBeInTheDocument();
    });
  });

  it('per-row menu surfaces «Дублировать» link with ?clone_from= query', async () => {
    renderSection({ roles: [MARKETING_ROLE] });
    const triggers = await screen.findAllByLabelText(/Действия для роли/);
    await userEvent.setup().click(triggers[0]);
    await waitFor(() => {
      const dup = screen.getByRole('menuitem', { name: 'Дублировать' });
      const anchor = dup.closest('a') ?? dup.querySelector('a');
      expect(anchor?.getAttribute('href')).toBe(
        `/settings/roles/new?clone_from=${MARKETING_ROLE.id}`
      );
    });
  });
});
