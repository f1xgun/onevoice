import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/lib/api/permissions', () => ({
  getPermissionsCatalog: vi.fn(),
  getMyPermissions: vi.fn(),
}));

import { TooltipProvider } from '@/components/ui/tooltip';
import { getPermissionsCatalog } from '@/lib/api/permissions';
import type { PermissionGroup } from '@/lib/schemas';

import { PermissionTree } from '../PermissionTree';

// Integration tests for expand-by-default, Info-icon tooltips, and catalog-
// driven rendering. The mock catalog uses arbitrary resource/permission
// names — the tree must render whatever the catalog returns, never
// hardcoded names.

const mockedGetCatalog = vi.mocked(getPermissionsCatalog);

const CATALOG: PermissionGroup[] = [
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
      { name: 'roles.update', description: 'Изменять роли' },
    ],
  },
];

const ACTOR_FULL = new Set([
  'business.read',
  'business.update',
  'roles.read',
  'roles.create',
  'roles.update',
]);

function renderTree(
  props: Partial<React.ComponentProps<typeof PermissionTree>> = {},
  qcOverride?: QueryClient
) {
  const qc = qcOverride ?? new QueryClient({ defaultOptions: { queries: { retry: false } } });
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={qc}>
        <TooltipProvider delayDuration={0}>{children}</TooltipProvider>
      </QueryClientProvider>
    );
  }
  return render(
    <Wrapper>
      <PermissionTree value={[]} onChange={() => {}} actorPermissions={ACTOR_FULL} {...props} />
    </Wrapper>
  );
}

beforeEach(() => {
  mockedGetCatalog.mockReset();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('PermissionTree', () => {
  it('renders Skeleton block (aria-busy) while catalog is loading', () => {
    mockedGetCatalog.mockImplementation(() => new Promise(() => {}));
    const { container } = renderTree();
    expect(container.querySelector('[aria-busy="true"]')).not.toBeNull();
  });

  it('every group is expanded by default — all leaves visible without chevron click', async () => {
    mockedGetCatalog.mockResolvedValue(CATALOG);
    renderTree();
    await waitFor(() => {
      expect(screen.getByText('Видеть профиль организации')).toBeInTheDocument();
      expect(screen.getByText('Редактировать профиль организации')).toBeInTheDocument();
      expect(screen.getByText('Видеть роли и права')).toBeInTheDocument();
      expect(screen.getByText('Создавать роли')).toBeInTheDocument();
      expect(screen.getByText('Редактировать роли')).toBeInTheDocument();
    });
  });

  it('renders a safe group fallback and the catalog description for future permissions', async () => {
    mockedGetCatalog.mockResolvedValue([
      {
        resource: 'imaginary',
        permissions: [{ name: 'imaginary.action', description: 'тест' }],
      },
    ]);
    renderTree({ actorPermissions: new Set(['imaginary.action']) });
    await waitFor(() => {
      expect(screen.getByText('Другие возможности')).toBeInTheDocument();
      expect(screen.getByText('Другая возможность')).toBeInTheDocument();
      expect(screen.queryByText('imaginary.action')).not.toBeInTheDocument();
    });
  });

  it('toggling a leaf fires onChange with the updated permissions array', async () => {
    mockedGetCatalog.mockResolvedValue(CATALOG);
    const onChange = vi.fn();
    renderTree({ onChange });
    await waitFor(() => expect(screen.getByText('Видеть профиль организации')).toBeInTheDocument());
    await userEvent
      .setup()
      .click(screen.getByRole('checkbox', { name: 'Видеть профиль организации' }));
    expect(onChange).toHaveBeenCalledWith(['business.read']);
  });

  it('wiring: clicking a group checkbox toggles all enabled leaves at once', async () => {
    mockedGetCatalog.mockResolvedValue(CATALOG);
    const onChange = vi.fn();
    renderTree({
      onChange,
      actorPermissions: new Set(['roles.read', 'roles.update']), // partial
    });
    await waitFor(() => expect(screen.getByText('Видеть роли и права')).toBeInTheDocument());
    await userEvent.setup().click(screen.getByRole('checkbox', { name: 'Роли и доступ' }));
    expect(onChange).toHaveBeenCalledTimes(1);
    const next = onChange.mock.calls[0][0] as string[];
    expect([...next].sort()).toEqual(['roles.read', 'roles.update'].sort());
  });

  it('disabled leaf explains that the actor cannot grant it', async () => {
    mockedGetCatalog.mockResolvedValue(CATALOG);
    renderTree({
      actorPermissions: new Set(['business.read', 'business.update', 'roles.read', 'roles.update']),
    });
    await waitFor(() => expect(screen.getByText('Создавать роли')).toBeInTheDocument());
    expect(screen.getByText('У вас нет этого права')).toBeInTheDocument();
  });
});
