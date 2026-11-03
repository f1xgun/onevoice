import { beforeEach, expect, it, vi } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { IntegrationsSyncPanel, syncStatusFor } from '../IntegrationsSyncPanel';
import {
  fetchIntegrationsDrift,
  verifyIntegrations,
  adoptIntegrationHours,
  restoreIntegrationHours,
} from '@/lib/api/integrationsSync';
import { toast } from 'sonner';

vi.mock('@/lib/api/integrationsSync', () => ({
  fetchIntegrationsDrift: vi.fn(),
  verifyIntegrations: vi.fn(),
  adoptIntegrationHours: vi.fn(),
  restoreIntegrationHours: vi.fn(),
}));
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
let canEdit = true;
vi.mock('@/lib/hooks/usePermission', () => ({
  usePermission: () => ({ allowed: canEdit, isLoading: false }),
}));
const integrations = [
  {
    id: 'tg',
    platform: 'telegram',
    externalId: 'private-id',
    metadata: { channel_title: 'Зёрна на Бажова' },
  },
  {
    id: 'ya',
    platform: 'yandex_business',
    externalId: 'org',
    metadata: { business_name: 'Кофейня Зёрна' },
  },
];
const week = Object.fromEntries(
  ['monday', 'tuesday', 'wednesday', 'thursday', 'friday', 'saturday', 'sunday'].map((day) => [
    day,
    { open: day === 'saturday' ? '10:00' : '08:00', close: '21:00' },
  ])
);
const stored = { ...week, saturday: { open: '09:00', close: '21:00' } };
const row = {
  platform: 'yandex_business',
  externalId: 'org',
  driftDetected: true,
  driftFields: ['schedule'],
  lastCheckedAt: '2026-11-03T07:00:00+03:00',
  nextCheckAt: '2026-11-04T07:00:00+03:00',
  remoteValues: { schedule: JSON.stringify(week) },
  storedValues: { schedule: JSON.stringify(stored) },
};
function mount() {
  return render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <IntegrationsSyncPanel businessId="business" integrations={integrations} />
    </QueryClientProvider>
  );
}
beforeEach(() => {
  vi.clearAllMocks();
  canEdit = true;
  vi.mocked(fetchIntegrationsDrift).mockResolvedValue([row]);
  vi.mocked(adoptIntegrationHours).mockResolvedValue({});
  vi.mocked(restoreIntegrationHours).mockResolvedValue({});
  vi.mocked(verifyIntegrations).mockResolvedValue({ started: true });
});
it('shows the actual weekly-hours difference with two explicit owner choices', async () => {
  mount();
  expect(await screen.findByText('Часы на Картах другие')).toBeVisible();
  expect(screen.getByText('Суббота')).toBeVisible();
  expect(screen.getByText('10:00–21:00')).toHaveClass('bg-diff-old');
  expect(screen.getByText('9:00–21:00')).toHaveClass('bg-diff-new');
  expect(screen.getByRole('button', { name: 'Вернуть ваши часы на Карты' })).toBeEnabled();
  expect(screen.getByRole('button', { name: 'Взять часы с Карт' })).toBeEnabled();
  expect(screen.queryByText('schedule')).toBeNull();
  expect(screen.queryByText('org')).toBeNull();
});
it('adopts the displayed source snapshot without invoking either outbound repair path', async () => {
  mount();
  await userEvent.click(await screen.findByRole('button', { name: 'Взять часы с Карт' }));
  await waitFor(() =>
    expect(adoptIntegrationHours).toHaveBeenCalledWith('business', 'org', row.lastCheckedAt)
  );
  expect(restoreIntegrationHours).not.toHaveBeenCalled();
  expect(verifyIntegrations).not.toHaveBeenCalled();
});
it('restores only hours instead of the full-profile verify operation', async () => {
  mount();
  await userEvent.click(await screen.findByRole('button', { name: 'Вернуть ваши часы на Карты' }));
  await waitFor(() => expect(restoreIntegrationHours).toHaveBeenCalledWith('business'));
  expect(verifyIntegrations).not.toHaveBeenCalled();
  expect(adoptIntegrationHours).not.toHaveBeenCalled();
});
it('keeps full-profile confirmation for a scalar difference and excludes raw identifiers', async () => {
  vi.mocked(fetchIntegrationsDrift).mockResolvedValue([
    {
      ...row,
      platform: 'telegram',
      externalId: 'private-id',
      driftFields: ['title'],
      remoteValues: { title: 'Зерна кофе' },
      storedValues: { title: 'Кофейня Зёрна' },
    },
  ]);
  mount();
  await userEvent.click(
    await screen.findByRole('button', { name: 'Вернуть данные из карточки на площадки' })
  );
  const dialog = await screen.findByRole('alertdialog');
  expect(dialog).toHaveTextContent('Зёрна на Бажова');
  expect(dialog).not.toHaveTextContent('private-id');
  await userEvent.click(within(dialog).getByRole('button', { name: 'Поправить везде' }));
  await waitFor(() => expect(verifyIntegrations).toHaveBeenCalled());
});
it('does not offer writes to a read-only actor', async () => {
  canEdit = false;
  mount();
  await screen.findByText('Часы на Картах другие');
  expect(screen.queryByRole('button')).toBeNull();
});
it('shows one positive receipt only when every connected channel has a successful comparison', async () => {
  vi.mocked(fetchIntegrationsDrift).mockResolvedValue(
    integrations.map((integration) => ({
      ...row,
      platform: integration.platform,
      externalId: integration.externalId,
      driftDetected: false,
      driftFields: [],
    }))
  );
  mount();
  expect(await screen.findByText(/проверенные данные совпадают/)).toBeVisible();
  expect(screen.queryByRole('button')).toBeNull();
});
it('does not turn missing, pending or failed comparison data into a green guarantee', async () => {
  vi.mocked(fetchIntegrationsDrift).mockResolvedValue([
    { ...row, driftDetected: false, checkFailed: true },
  ]);
  mount();
  await waitFor(() => expect(fetchIntegrationsDrift).toHaveBeenCalled());
  expect(screen.queryByText(/совпадают/)).toBeNull();
  expect(syncStatusFor({ ...row, driftDetected: false, checkFailed: true })).toBe('not_checked');
  expect(syncStatusFor(undefined)).toBe('not_checked');
});
it('keeps a loading skeleton and a fetch failure distinct from a completed comparison', async () => {
  vi.mocked(fetchIntegrationsDrift).mockRejectedValue(new Error('offline'));
  mount();
  expect(await screen.findByRole('alert')).toBeVisible();
  expect(screen.queryByText(/совпадают/)).toBeNull();
});
it('keeps the difference available after a failed mutation', async () => {
  vi.mocked(adoptIntegrationHours).mockRejectedValue(new Error('offline'));
  mount();
  await userEvent.click(await screen.findByRole('button', { name: 'Взять часы с Карт' }));
  await waitFor(() => expect(toast.error).toHaveBeenCalled());
  expect(screen.getByText('10:00–21:00')).toBeVisible();
});
