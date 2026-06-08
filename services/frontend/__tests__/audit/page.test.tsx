import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

// Mock the bare axios `api` client used by useAuditLogs.fetchPage.
const apiGet = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    get: (url: string) => apiGet(url),
  },
}));

// Mock the active business so the page is enabled.
vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (selector: (s: { activeBusinessId: string | null }) => unknown) =>
    selector({ activeBusinessId: 'biz-1' }),
}));

// Bypass the RBAC gate so the test exercises page rendering, not
// permission logic (that's tested separately in permission/__tests__).
vi.mock('@/components/permission/RequirePermission', () => ({
  RequirePermission: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

// Skip members fetch — Autocomplete renders an empty option list.
vi.mock('@/lib/hooks/useMembers', () => ({
  useMembers: () => ({ data: [], isLoading: false }),
}));

// Calendar pulls in DayPicker (date-fns + DOM). Replace with a stub so
// the page test doesn't have to assert calendar internals.
vi.mock('@/components/ui/calendar', () => ({
  Calendar: () => <div data-testid="calendar-stub" />,
}));

import AuditPage from '@/app/(app)/settings/audit/page';

function makeClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function renderPage() {
  const client = makeClient();
  return render(
    <QueryClientProvider client={client}>
      <AuditPage />
    </QueryClientProvider>
  );
}

describe('AuditPage', () => {
  beforeEach(() => {
    apiGet.mockReset();
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  it('renders title, subtitle, and empty state when no items', async () => {
    apiGet.mockResolvedValue({ data: { items: [], next_cursor: null } });
    renderPage();
    expect(await screen.findByRole('heading', { name: 'Журнал событий' })).toBeInTheDocument();
    expect(screen.getByText(/Все изменения ролей/)).toBeInTheDocument();
    expect(await screen.findByTestId('audit-table-empty')).toBeInTheDocument();
    expect(screen.getByText('Событий за выбранный период пока нет.')).toBeInTheDocument();
  });

  it('renders rows and opens the side panel on row click', async () => {
    const { default: userEvent } = await import('@testing-library/user-event');
    apiGet.mockResolvedValue({
      data: {
        items: [
          {
            id: 'a-1',
            action: 'rbac.role_granted',
            action_category: 'rbac',
            resource: 'role',
            business_id: 'biz-1',
            actor_id: 'u-1',
            actor_email: 'alice@example.com',
            actor_display_name: null,
            details: { target_user_id: 'u-2', new_role_id: 'r-9' },
            created_at: new Date(Date.now() - 5 * 60 * 1000).toISOString(),
          },
        ],
        next_cursor: null,
      },
    });
    renderPage();
    const row = await screen.findByTestId('audit-row-a-1');
    expect(row).toBeInTheDocument();
    expect(row).toHaveTextContent('alice@example.com');
    expect(row).toHaveTextContent('Роль');
    expect(row).not.toHaveTextContent('role');
    await userEvent.setup().click(row);
    await waitFor(() => {
      expect(screen.getByTestId('panel-actor')).toBeInTheDocument();
    });
    expect(screen.getByTestId('panel-actor')).toHaveTextContent('alice@example.com');
    expect(screen.getByTestId('panel-raw-json')).toHaveTextContent('target_user_id');
  });

  it('prefers the actor display name over the email when present', async () => {
    apiGet.mockResolvedValue({
      data: {
        items: [
          {
            id: 'n-1',
            action: 'rbac.role_granted',
            action_category: 'rbac',
            resource: 'role',
            business_id: 'biz-1',
            actor_id: 'u-1',
            actor_email: 'alice@example.com',
            actor_display_name: 'Алиса Лидделл',
            details: {},
            created_at: new Date().toISOString(),
          },
        ],
        next_cursor: null,
      },
    });
    renderPage();
    const row = await screen.findByTestId('audit-row-n-1');
    expect(row).toHaveTextContent('Алиса Лидделл');
    expect(row).not.toHaveTextContent('alice@example.com');
  });

  it('falls back to the raw resource string for an unmapped resource', async () => {
    apiGet.mockResolvedValue({
      data: {
        items: [
          {
            id: 'u-9',
            action: 'auth.login_failed',
            action_category: 'auth',
            resource: 'mystery_widget',
            business_id: null,
            actor_id: null,
            actor_email: null,
            actor_display_name: null,
            details: { attempted_email: 'x@y.z' },
            created_at: new Date().toISOString(),
          },
        ],
        next_cursor: null,
      },
    });
    renderPage();
    const row = await screen.findByTestId('audit-row-u-9');
    expect(row).toHaveTextContent('mystery_widget');
  });

  it('renders failed-login actor as "Неизвестен ({email})"', async () => {
    apiGet.mockResolvedValue({
      data: {
        items: [
          {
            id: 'f-1',
            action: 'auth.login_failed',
            action_category: 'auth',
            resource: 'auth',
            business_id: null,
            actor_id: null,
            actor_email: null,
            actor_display_name: null,
            details: {
              attempted_email: 'mallory@example.com',
              ip: '1.2.3.4',
              user_agent: 'curl',
              reason: 'invalid_credentials',
            },
            created_at: new Date().toISOString(),
          },
        ],
        next_cursor: null,
      },
    });
    renderPage();
    const row = await screen.findByTestId('audit-row-f-1');
    expect(row).toHaveTextContent('Неизвестен (mallory@example.com)');
  });

  it('shows the Load more button when next_cursor is non-null', async () => {
    apiGet.mockResolvedValue({
      data: {
        items: [
          {
            id: 'a-2',
            action: 'auth.login_success',
            action_category: 'auth',
            resource: 'auth',
            business_id: 'biz-1',
            actor_id: 'u-1',
            actor_email: 'alice@example.com',
            actor_display_name: null,
            details: { ip: '1.2.3.4', user_agent: 'curl' },
            created_at: new Date().toISOString(),
          },
        ],
        next_cursor: 'cursor-page-2',
      },
    });
    renderPage();
    await screen.findByTestId('audit-row-a-2');
    expect(screen.getByRole('button', { name: 'Загрузить ещё' })).toBeInTheDocument();
  });
});
