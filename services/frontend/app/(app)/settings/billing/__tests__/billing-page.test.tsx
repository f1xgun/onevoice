import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor, cleanup } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

vi.mock('@/lib/api/billing', () => ({
  getBillingSummary: vi.fn(),
}));
vi.mock('@/lib/api/permissions', () => ({
  getMyPermissions: vi.fn(),
}));

import { useBusinessStore } from '@/lib/stores/business';
import { getBillingSummary, type BillingSummary } from '@/lib/api/billing';
import { getMyPermissions } from '@/lib/api/permissions';
import BillingPage from '../page';

const mockedGetSummary = vi.mocked(getBillingSummary);
const mockedGetMyPerms = vi.mocked(getMyPermissions);

const SUMMARY: BillingSummary = {
  plan: { code: 'pro', name: 'Pro', monthly_credits: 2000 },
  credits: { granted: 2000, used: 500, remaining: 1500, overage: 0 },
  usage_this_month: { actions: 42, spend_usd: 12.34, images: 7 },
  daily_spend: { today_usd: 1.5, cap_usd: 5 },
};

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  }
  return render(
    <Wrapper>
      <BillingPage />
    </Wrapper>
  );
}

beforeEach(() => {
  mockedGetSummary.mockReset();
  mockedGetMyPerms.mockReset();
  mockedGetSummary.mockResolvedValue(SUMMARY);
  mockedGetMyPerms.mockResolvedValue(['billing.read']);
  useBusinessStore.setState({ activeBusinessId: 'biz-1' });
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe('BillingPage', () => {
  it('renders plan, credits, usage and daily spend for an allowed actor', async () => {
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Pro')).toBeInTheDocument();
    });
    expect(screen.getByText('pro')).toBeInTheDocument();
    expect(screen.getByText('1500')).toBeInTheDocument();
    expect(screen.getByText('42')).toBeInTheDocument();
    expect(screen.getByText('$12.34')).toBeInTheDocument();
    expect(screen.getByText('7')).toBeInTheDocument();
    expect(screen.getByText('$1.50')).toBeInTheDocument();
    expect(screen.getByText('$5.00')).toBeInTheDocument();
  });

  it('shows the forbidden fallback when the actor lacks billing.read', async () => {
    mockedGetMyPerms.mockResolvedValue([]);
    renderPage();
    await waitFor(() => {
      expect(
        screen.getByText('У вас нет доступа к разделу тарифа и использования.')
      ).toBeInTheDocument();
    });
    expect(screen.queryByText('Pro')).not.toBeInTheDocument();
  });

  it('renders a loading state while the summary is pending', async () => {
    mockedGetSummary.mockImplementation(() => new Promise(() => {}));
    renderPage();
    await waitFor(() => {
      expect(document.querySelector('[aria-busy="true"]')).not.toBeNull();
    });
  });

  it('renders an error state when the summary fetch fails', async () => {
    mockedGetSummary.mockRejectedValue(new Error('network'));
    renderPage();
    await waitFor(() => {
      expect(
        screen.getByText('Не удалось загрузить данные о тарифе. Обновите страницу.')
      ).toBeInTheDocument();
    });
  });

  it('renders an unlimited daily cap without $-1.00 and hides the daily gauge', async () => {
    mockedGetSummary.mockResolvedValue({
      plan: { code: 'enterprise', name: 'Enterprise', monthly_credits: 20000 },
      credits: { granted: 20000, used: 0, remaining: 20000, overage: 0 },
      usage_this_month: { actions: 100, spend_usd: 50, images: 3 },
      daily_spend: { today_usd: 12.5, cap_usd: -1 },
    });
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Enterprise')).toBeInTheDocument();
    });
    expect(screen.getByText('Безлимит')).toBeInTheDocument();
    expect(screen.queryByText('$-1.00')).not.toBeInTheDocument();
    expect(
      screen.queryByRole('progressbar', {
        name: 'Расход за сегодня относительно дневного лимита',
      })
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole('progressbar', { name: 'Использовано кредитов из начисленных' })
    ).toBeInTheDocument();
  });

  it('renders unlimited monthly credits and hides the credits gauge', async () => {
    mockedGetSummary.mockResolvedValue({
      plan: { code: 'enterprise', name: 'Enterprise', monthly_credits: -1 },
      credits: { granted: -1, used: 0, remaining: 0, overage: 0 },
      usage_this_month: { actions: 100, spend_usd: 50, images: 3 },
      daily_spend: { today_usd: 1, cap_usd: 5 },
    });
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Enterprise')).toBeInTheDocument();
    });
    expect(screen.getAllByText('Безлимит').length).toBeGreaterThanOrEqual(2);
    expect(screen.queryByText('-1')).not.toBeInTheDocument();
    expect(
      screen.queryByRole('progressbar', { name: 'Использовано кредитов из начисленных' })
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole('progressbar', { name: 'Расход за сегодня относительно дневного лимита' })
    ).toBeInTheDocument();
  });
});
