import { mkdirSync } from 'node:fs';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useAuthStore } from '@/lib/auth';
import { useBusinessStore } from '@/lib/stores/business';
import { hasLayoutBrowser, withLayoutPage } from '@/test-utils/browser-layout';
import SettingsPage from '../page';
import AccountPage from '../account/page';
import TeamPage from '../team/page';
import BillingPage from '../billing/page';
import DataPage from '../data/page';

const state = vi.hoisted(() => ({ roleId: 'admin' }));
vi.mock('@/lib/hooks/usePermission', () => ({
  usePermission: () => ({ allowed: true, isLoading: false }),
}));
vi.mock('@/lib/hooks/useBusinessList', () => ({
  useBusinessList: () => ({
    data: [{ id: 'biz-1', name: 'Кофейня «Зёрна»', role: { id: state.roleId, name: 'admin' } }],
  }),
}));
vi.mock('@/lib/hooks/useMembers', () => ({
  useMembers: () => ({
    data: [
      {
        user: { id: 'user-1', name: 'Анна Котельникова', email: 'anna@zerna-kofe.ru' },
        role: { id: 'admin', name: 'admin' },
        joined_at: '2026-09-14T12:00:00Z',
      },
    ],
  }),
  useRoles: () => ({
    data: [
      {
        id: 'admin',
        name: 'admin',
        description: 'Управляет участниками и подключениями',
        is_system: true,
        permissions: [],
      },
    ],
  }),
  useUpdateMemberRole: () => ({ mutateAsync: vi.fn() }),
  useRemoveMember: () => ({ mutateAsync: vi.fn() }),
}));
vi.mock('@/lib/hooks/useInvitations', () => ({
  useInvitations: () => ({ data: [], isLoading: false }),
  useCreateInvitation: () => ({ mutateAsync: vi.fn() }),
  useRevokeInvitation: () => ({ mutateAsync: vi.fn() }),
}));
vi.mock('@/lib/api/consents', () => ({
  listMyConsents: async () =>
    ['tos', 'privacy', 'pdn'].map((slug) => ({
      slug,
      acceptedAt: '2026-09-14T12:00:00Z',
      version: '2026-09-12',
    })),
  withdrawPDN: vi.fn(),
}));
vi.mock('../billing/_lib/useBillingSummary', () => ({
  useBillingSummary: () => ({
    isSuccess: true,
    data: {
      plan: { code: 'free', name: 'Free', monthly_credits: 100 },
      credits: { granted: 100, used: 37, remaining: 63, overage: 0 },
      usage_this_month: {
        actions: 24,
        images: 4,
        spend_usd: 0,
        units: { total: 37, text: 33, images: 4, other: 0 },
      },
      daily_spend: { today_usd: 0, cap_usd: 5 },
    },
  }),
}));
vi.mock('../audit/_lib/useAuditLogs', () => ({
  useAuditLogs: () => ({
    items: [
      {
        id: 'event-1',
        action: 'rbac.invitation_created',
        action_category: 'rbac',
        resource: 'invitation',
        actor_display_name: 'Анна',
        details: {},
        created_at: '2026-11-01T12:16:00Z',
      },
    ],
    isLoading: false,
  }),
}));
vi.mock('@/components/business/DeleteBusinessModal', () => ({
  DeleteBusinessModal: ({ open }: { open: boolean }) =>
    open ? <p data-testid="delete-business-modal" /> : null,
}));

beforeEach(() => {
  state.roleId = 'admin';
  useBusinessStore.setState({ activeBusinessId: 'biz-1' });
  useAuthStore.setState({
    user: { id: 'user-1', name: 'Анна Котельникова', email: 'anna@zerna-kofe.ru' },
    accessToken: 'token',
  });
});

function renderPage(Page: typeof SettingsPage) {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <div className="ov-app">
        <Page />
      </div>
    </QueryClientProvider>
  );
}

describe('settings v2', () => {
  it('links to the five settings destinations', () => {
    renderPage(SettingsPage);
    for (const destination of ['rules', 'team', 'billing', 'account', 'data']) {
      expect(document.querySelector(`a[href="/settings/${destination}"]`)).toBeInTheDocument();
    }
  });

  it('keeps location deletion disabled for an administrator', async () => {
    renderPage(DataPage);
    expect(await screen.findByRole('button', { name: 'Удалить организацию' })).toBeDisabled();
    expect(screen.getByText('Удалить организацию может только владелец.')).toBeInTheDocument();
    expect(screen.queryByTestId('delete-business-modal')).toBeNull();
  });

  it('opens the existing confirmation only for the owner', async () => {
    state.roleId = '00000000-0000-0000-0000-000000000001';
    renderPage(DataPage);
    await userEvent.click(await screen.findByRole('button', { name: 'Удалить организацию' }));
    expect(screen.getByTestId('delete-business-modal')).toBeInTheDocument();
  });

  it.skipIf(!hasLayoutBrowser).each([
    ['settings', SettingsPage],
    ['account', AccountPage],
    ['team', TeamPage],
    ['invite', TeamPage],
    ['billing', BillingPage],
    ['data', DataPage],
  ] as const)(
    'keeps %s usable at phone and desktop widths in both themes',
    async (name, Page) => {
      renderPage(Page);
      if (name === 'data') await screen.findByText('Пользовательское соглашение');
      if (name === 'invite')
        await userEvent.click(screen.getByRole('button', { name: 'Пригласить' }));
      document.querySelectorAll('input').forEach((input) => {
        input.setAttribute('value', input.value);
        if (input.checked) input.setAttribute('checked', '');
      });
      mkdirSync('/private/tmp/onevoice-redesign/settings', { recursive: true });
      for (const width of [375, 1280]) {
        await withLayoutPage(document.body.innerHTML, { width, height: 900 }, async (page) => {
          for (const theme of ['light', 'dark']) {
            await page.locator('html').evaluate((html, value) => {
              html.className = value;
            }, theme);
            expect(
              await page.locator('body').evaluate((body) => body.scrollWidth)
            ).toBeLessThanOrEqual(width);
            if (name === 'invite') {
              const sheet = (await page.getByRole('dialog').boundingBox())!;
              expect(sheet.y + sheet.height).toBe(900);
              expect(sheet.width).toBeLessThanOrEqual(width);
            }
            await page.screenshot({
              path: `/private/tmp/onevoice-redesign/settings/${name}-${width}-${theme}.png`,
              fullPage: true,
            });
          }
        });
      }
    },
    20000
  );
});
