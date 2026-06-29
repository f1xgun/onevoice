import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { HoursForm, SpecialDatesForm } from '@/components/business/ScheduleForm';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import type { Business, ScheduleDay, SpecialDate } from '@/types/business';

const BIZ_ID = 'test-biz-id';

const bizApiPut = vi.fn();
vi.mock('@/lib/api/business-api', () => ({
  bizApi: (bizId: string) => ({
    get: vi.fn(),
    post: vi.fn(),
    put: (path: string, data?: unknown) => bizApiPut(bizId, path, data),
    patch: vi.fn(),
    delete: vi.fn(),
  }),
}));

vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (selector: (s: { activeBusinessId: string | null }) => unknown) =>
    selector({ activeBusinessId: BIZ_ID }),
}));

vi.mock('@/lib/hooks/usePermission', () => ({
  usePermission: () => ({ allowed: true, isLoading: false }),
}));

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

const INITIAL_HOURS: ScheduleDay[] = [
  { day: 'mon', open: '09:00', close: '21:00', closed: false },
  { day: 'tue', open: '09:00', close: '21:00', closed: false },
  { day: 'wed', open: '09:00', close: '21:00', closed: false },
  { day: 'thu', open: '09:00', close: '21:00', closed: false },
  { day: 'fri', open: '09:00', close: '21:00', closed: false },
  { day: 'sat', open: '10:00', close: '21:00', closed: false },
  { day: 'sun', open: '10:00', close: '20:00', closed: true },
];

const SAVED_SPECIAL_DATES: SpecialDate[] = [{ date: '2026-03-08', closed: true }];

function makeClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function wrapper(client: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

function lastPutPayload(): { schedule: ScheduleDay[]; specialDates: SpecialDate[] } {
  const calls = bizApiPut.mock.calls;
  return calls[calls.length - 1][2] as { schedule: ScheduleDay[]; specialDates: SpecialDate[] };
}

describe('Schedule lost-update across independent Hours / Special Dates forms', () => {
  beforeEach(() => {
    bizApiPut.mockReset();
    bizApiPut.mockResolvedValue({ data: {} });
  });

  it('a Hours save preserves the previously-saved special dates instead of clobbering them', async () => {
    const user = userEvent.setup();
    const client = makeClient();

    // The page first renders both forms with NO special dates persisted yet:
    // this is what freezes the HoursForm instance with an empty specialDates.
    render(
      <>
        <HoursForm initialSchedule={INITIAL_HOURS} initialSpecialDates={[]} />
        <SpecialDatesForm initialSchedule={INITIAL_HOURS} initialSpecialDates={[]} />
      </>,
      { wrapper: wrapper(client) }
    );

    // User adds + saves a special date. Its save persists the special dates,
    // and the post-save refetch lands them in the BUSINESS_PROFILE cache. We
    // model that refetched profile directly so the test does not depend on a
    // live backend round-trip.
    client.setQueryData<Business>(QUERY_KEYS.BUSINESS_PROFILE(BIZ_ID), {
      id: BIZ_ID,
      name: 'Acme',
      category: 'cafe',
      settings: { schedule: { schedule: INITIAL_HOURS, specialDates: SAVED_SPECIAL_DATES } },
    });

    // Now the user saves Hours from the still-mounted HoursForm, whose local
    // specialDates copy is the stale empty array.
    await user.click(screen.getByRole('button', { name: 'Сохранить часы' }));

    const payload = lastPutPayload();
    expect(payload.specialDates).toEqual(SAVED_SPECIAL_DATES);
    expect(payload.specialDates).not.toHaveLength(0);
    expect(payload.schedule).toEqual(INITIAL_HOURS);
  });
});
