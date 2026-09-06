import type * as ReactQuery from '@tanstack/react-query';
import { expect, it, vi } from 'vitest';
import { render } from '@testing-library/react';
import BusinessPage from '../page';

const { state } = vi.hoisted(() => ({
  state: { isLoading: false, data: { name: 'Организация'.repeat(30) } },
}));
vi.mock('@tanstack/react-query', async (original) => ({
  ...(await original<typeof ReactQuery>()),
  useQuery: () => state,
}));
vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (select: (s: { activeBusinessId: string }) => unknown) =>
    select({ activeBusinessId: 'org' }),
}));
vi.mock('@/components/business/ProfileForm', () => ({
  ProfileForm: () => <p role="alert">{'Ошибка'.repeat(50)}</p>,
}));
vi.mock('@/components/business/ScheduleForm', () => ({
  HoursForm: () => null,
  SpecialDatesForm: () => null,
}));
vi.mock('@/components/business/VoiceToneSection', () => ({ VoiceToneSection: () => null }));
vi.mock('@/components/business/VoiceProfileSection', () => ({ VoiceProfileSection: () => null }));
vi.mock('@/components/business/DangerZone', () => ({ DangerZone: () => null }));
vi.mock('@/components/business/BusinessDeletionGraceBanner', () => ({
  BusinessDeletionGraceBanner: () => null,
}));
vi.mock('@/components/onboarding/SectionHelp', () => ({ SectionHelp: () => null }));

it.each([false, true])('uses shrinking tracks and a wide breakpoint (loading=%s)', (isLoading) => {
  state.isLoading = isLoading;
  const { container } = render(<BusinessPage />);
  const grid = container.querySelector('.grid');
  expect(grid).toHaveClass('xl:grid-cols-[minmax(0,1fr)_minmax(0,320px)]');
  expect(grid).not.toHaveClass('lg:grid-cols-[1fr_320px]');
});
