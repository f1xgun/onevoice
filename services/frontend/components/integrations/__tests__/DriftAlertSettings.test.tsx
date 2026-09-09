import { fireEvent, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { DriftAlertSettings } from '../DriftAlertSettings';

const mocks = vi.hoisted(() => ({
  activeBusinessId: 'business-a',
  read: { allowed: false, isLoading: true, isError: false, refetch: vi.fn() },
  update: { allowed: false, isLoading: true, isError: false, refetch: vi.fn() },
  query: {
    data: undefined as { enabled: boolean; locale: 'ru' | 'en' } | undefined,
    isLoading: false,
    isError: false,
    isSuccess: false,
    refetch: vi.fn(),
  },
  mutate: vi.fn(),
}));

vi.mock('next-intl', () => ({
  useLocale: () => 'en',
  useTranslations: () => (key: string) => key,
}));

vi.mock('@/lib/hooks/usePermission', () => ({
  usePermission: (permission: string) =>
    permission === 'business.read' ? mocks.read : mocks.update,
}));

vi.mock('@/lib/hooks/useDriftAlerts', () => ({
  useDriftAlerts: () => mocks.query,
  useUpdateDriftAlerts: () => ({ mutate: mocks.mutate, isPending: false }),
}));

vi.mock('@/lib/stores/business', () => {
  const useBusinessStore = (selector: (state: { activeBusinessId: string | null }) => unknown) =>
    selector({ activeBusinessId: mocks.activeBusinessId });
  useBusinessStore.getState = () => ({ activeBusinessId: mocks.activeBusinessId });
  return { useBusinessStore };
});

describe('DriftAlertSettings', () => {
  beforeEach(() => {
    mocks.activeBusinessId = 'business-a';
    Object.assign(mocks.read, { allowed: false, isLoading: true, isError: false });
    Object.assign(mocks.update, { allowed: false, isLoading: true, isError: false });
    Object.assign(mocks.query, {
      data: undefined,
      isLoading: false,
      isError: false,
      isSuccess: false,
    });
    mocks.read.refetch.mockReset();
    mocks.query.refetch.mockReset();
    mocks.mutate.mockReset();
  });

  it('shows permission loading and a retryable permission error', () => {
    const view = render(<DriftAlertSettings businessId="business-a" />);
    expect(screen.getByRole('status', { hidden: true })).toHaveAttribute(
      'data-loading-placeholder',
      'pending'
    );

    Object.assign(mocks.read, { isLoading: false, isError: true });
    view.rerender(<DriftAlertSettings businessId="business-a" />);
    fireEvent.click(screen.getByRole('button', { name: 'retry' }));
    expect(mocks.read.refetch).toHaveBeenCalledOnce();
  });

  it('shows a retryable settings read error', () => {
    Object.assign(mocks.read, { allowed: true, isLoading: false, isError: false });
    Object.assign(mocks.query, { isError: true });
    render(<DriftAlertSettings businessId="business-a" />);

    fireEvent.click(screen.getByRole('button', { name: 'retry' }));
    expect(mocks.query.refetch).toHaveBeenCalledOnce();
  });

  it('keeps controls disabled without update permission after a successful read', () => {
    Object.assign(mocks.read, { allowed: true, isLoading: false, isError: false });
    Object.assign(mocks.update, { allowed: false, isLoading: false, isError: false });
    Object.assign(mocks.query, {
      data: { enabled: false, locale: 'en' },
      isSuccess: true,
    });
    render(<DriftAlertSettings businessId="business-a" />);

    expect(screen.getByRole('switch')).toBeDisabled();
    expect(screen.queryByRole('button', { name: 'save' })).not.toBeInTheDocument();
  });
});
