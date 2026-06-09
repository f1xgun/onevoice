import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { AuditTable } from '@/app/(app)/settings/audit/_components/AuditTable';
import type { AuditLogDTO } from '@/app/(app)/settings/audit/_lib/types';

// Renders the audit journal table through the global next-intl mock (RU
// locale), so the assertions pin the actual user-facing strings — including
// the "Система" actor for system events and the readable action labels that
// replace the raw `audit.actions.*` key fallthrough.

function makeRow(over: Partial<AuditLogDTO>): AuditLogDTO {
  return {
    id: 'row-1',
    action: 'rbac.role_granted',
    action_category: 'rbac',
    resource: 'role',
    business_id: 'biz-1',
    actor_id: null,
    actor_email: null,
    actor_display_name: null,
    details: {},
    created_at: new Date().toISOString(),
    ...over,
  } as AuditLogDTO;
}

function renderTable(items: AuditLogDTO[]) {
  render(
    <AuditTable
      items={items}
      isLoading={false}
      hasNextPage={false}
      isFetchingMore={false}
      onLoadMore={vi.fn()}
      onRowClick={vi.fn()}
    />
  );
}

describe('AuditTable actor + action labels', () => {
  it('renders a readable label for token_decrypted (no raw key) and "Система" actor', () => {
    renderTable([
      makeRow({
        id: 'r-decrypt',
        action: 'integration.token_decrypted',
        action_category: 'integration',
        resource: 'integration',
        details: { platform: 'vk', reason: 'vk__publish_post' },
      }),
    ]);

    expect(screen.getByText('Доступ к токену интеграции')).toBeInTheDocument();
    expect(screen.queryByText(/audit\.actions\./)).not.toBeInTheDocument();
    expect(screen.getByText('Система')).toBeInTheDocument();
  });

  it('shows the editable display name when present', () => {
    renderTable([makeRow({ actor_display_name: 'Анна П.', actor_email: 'anna@test.local' })]);
    expect(screen.getByText('Анна П.')).toBeInTheDocument();
  });

  it('keeps the attempted-email fallback for failed logins (not "Система")', () => {
    renderTable([
      makeRow({
        id: 'r-login',
        action: 'auth.login_failed',
        action_category: 'auth',
        resource: 'user',
        details: { attempted_email: 'probe@evil.test' },
      }),
    ]);

    expect(screen.getByText('Неизвестен (probe@evil.test)')).toBeInTheDocument();
    expect(screen.queryByText('Система')).not.toBeInTheDocument();
  });
});
