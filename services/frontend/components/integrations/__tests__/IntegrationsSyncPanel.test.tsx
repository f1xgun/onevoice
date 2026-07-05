import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor, cleanup } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

// Mocks must precede the component import so its transitive imports resolve
// to the mocked exports at module-evaluation time.
vi.mock('@/lib/api/integrationsSync', () => ({
  fetchIntegrationsDrift: vi.fn(),
  verifyIntegrations: vi.fn(),
}));
vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() },
}));
vi.mock('@/lib/telemetry', () => ({ trackClick: vi.fn() }));

let permAllowed = true;
vi.mock('@/lib/hooks/usePermission', () => ({
  usePermission: () => ({ allowed: permAllowed, isLoading: false }),
}));

import { toast } from 'sonner';
import { fetchIntegrationsDrift, verifyIntegrations } from '@/lib/api/integrationsSync';
import type { IntegrationDrift } from '@/lib/api/integrationsSync';
import { IntegrationsSyncPanel } from '../IntegrationsSyncPanel';

const mockedFetchDrift = vi.mocked(fetchIntegrationsDrift);
const mockedVerify = vi.mocked(verifyIntegrations);
const mockedToast = vi.mocked(toast);

const INTEGRATIONS = [
  {
    id: 't1',
    platform: 'telegram',
    externalId: '-100200',
    metadata: { channel_title: 'My Channel' },
  },
  { id: 'v1', platform: 'vk', externalId: '55', metadata: { community_name: 'My Community' } },
  { id: 'y1', platform: 'yandex_business', externalId: 'org-1', metadata: {} },
];

// telegram: checked, no drift → in sync.
// vk: checked, drifted on title+website → out of sync.
// yandex: pending row (no lastCheckedAt) → must read "not checked", never green.
const DRIFT: IntegrationDrift[] = [
  {
    platform: 'telegram',
    externalId: '-100200',
    driftDetected: false,
    driftFields: [],
    lastCheckedAt: '2026-07-01T10:00:00Z',
    nextCheckAt: '2026-07-02T10:00:00Z',
  },
  {
    platform: 'vk',
    externalId: '55',
    driftDetected: true,
    driftFields: ['title', 'website'],
    lastCheckedAt: '2026-07-01T10:00:00Z',
    nextCheckAt: '2026-07-02T10:00:00Z',
  },
  {
    platform: 'yandex_business',
    externalId: 'org-1',
    driftDetected: false,
    driftFields: [],
    nextCheckAt: '2026-07-02T10:00:00Z',
  },
];

function renderPanel() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  }
  return render(
    <Wrapper>
      <IntegrationsSyncPanel businessId="biz-1" integrations={INTEGRATIONS} />
    </Wrapper>
  );
}

beforeEach(() => {
  permAllowed = true;
  mockedFetchDrift.mockReset();
  mockedVerify.mockReset();
  mockedToast.success.mockReset();
  mockedToast.error.mockReset();
  mockedFetchDrift.mockResolvedValue(DRIFT);
  mockedVerify.mockResolvedValue({ status: 'repair_started' });
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe('IntegrationsSyncPanel', () => {
  it('renders in-sync, out-of-sync (with plain field names) and not-checked statuses', async () => {
    renderPanel();

    // Wait for the resolved status, not the prop-rendered channel name.
    await screen.findByText('Синхронизировано');
    expect(screen.getByText('My Channel')).toBeInTheDocument();

    // in sync (only telegram) — the pending yandex row must NOT read as green.
    expect(screen.getAllByText('Синхронизировано')).toHaveLength(1);

    // out of sync with human-readable field names.
    expect(screen.getByText('Рассинхронизация')).toBeInTheDocument();
    expect(screen.getByText('Отличается от площадки: название, сайт')).toBeInTheDocument();

    // pending (no lastCheckedAt) → neutral "not checked".
    expect(screen.getByText('Не проверялось')).toBeInTheDocument();
  });

  it('runs the verify flow: confirm → POST verify → success toast → drift refetch', async () => {
    const user = userEvent.setup();
    renderPanel();
    await waitFor(() => expect(screen.getByText('My Channel')).toBeInTheDocument());
    expect(mockedFetchDrift).toHaveBeenCalledTimes(1);

    await user.click(screen.getByRole('button', { name: 'Проверить синхронизацию' }));
    await user.click(await screen.findByRole('button', { name: 'Переотправить и проверить' }));

    await waitFor(() => expect(mockedVerify).toHaveBeenCalledWith('biz-1'));
    await waitFor(() => expect(mockedToast.success).toHaveBeenCalled());
    // invalidation refetches the drift query.
    await waitFor(() => expect(mockedFetchDrift.mock.calls.length).toBeGreaterThan(1));
  });

  it('shows an error toast when verify fails', async () => {
    mockedVerify.mockRejectedValue(new Error('boom'));
    const user = userEvent.setup();
    renderPanel();
    await waitFor(() => expect(screen.getByText('My Channel')).toBeInTheDocument());

    await user.click(screen.getByRole('button', { name: 'Проверить синхронизацию' }));
    await user.click(await screen.findByRole('button', { name: 'Переотправить и проверить' }));

    await waitFor(() => expect(mockedToast.error).toHaveBeenCalled());
    expect(mockedToast.success).not.toHaveBeenCalled();
  });

  it('hides the verify action for actors without business.update', async () => {
    permAllowed = false;
    renderPanel();
    // read-only status is still shown for viewers.
    await screen.findByText('Синхронизировано');

    expect(
      screen.queryByRole('button', { name: 'Проверить синхронизацию' })
    ).not.toBeInTheDocument();
  });

  it('renders a skeleton (no false green) while drift is loading', async () => {
    mockedFetchDrift.mockImplementation(() => new Promise(() => {}));
    renderPanel();
    // channels render immediately from props; statuses are still pending.
    await waitFor(() => expect(screen.getByText('My Channel')).toBeInTheDocument());
    expect(document.querySelectorAll('[data-state="static"]').length).toBeGreaterThan(0);
    expect(screen.queryByText('Синхронизировано')).not.toBeInTheDocument();
  });

  it('shows a load-failed notice when the drift fetch errors', async () => {
    mockedFetchDrift.mockRejectedValue(new Error('network'));
    renderPanel();
    await waitFor(() =>
      expect(screen.getByText('Не удалось загрузить статус синхронизации.')).toBeInTheDocument()
    );
    expect(screen.queryByText('Синхронизировано')).not.toBeInTheDocument();
  });
});
