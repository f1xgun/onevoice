import { render, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { ScopedBusinessLink } from '../ScopedBusinessLink';

const mocks = vi.hoisted(() => ({
  requestedId: null as string | null,
  memberships: [] as Array<{ id: string; deletion_pending_until?: string }>,
  setActive: vi.fn(),
}));

vi.mock('next/navigation', () => ({
  useSearchParams: () => ({ get: () => mocks.requestedId }),
}));

vi.mock('@/lib/hooks/useBusinessList', () => ({
  useBusinessList: () => ({ data: mocks.memberships }),
}));

vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (selector: (state: { setActive: typeof mocks.setActive }) => unknown) =>
    selector({ setActive: mocks.setActive }),
}));

const MEMBER_ID = '7b7d23dc-d21c-4e01-9f10-1f4bea92a1bf';

describe('ScopedBusinessLink', () => {
  beforeEach(() => {
    mocks.requestedId = null;
    mocks.memberships = [];
    mocks.setActive.mockReset();
    window.history.replaceState({}, '', '/integrations?businessId=' + MEMBER_ID + '&connected=vk');
  });

  it('switches to a UUID that exists in the membership response and consumes only that parameter', async () => {
    mocks.requestedId = MEMBER_ID;
    mocks.memberships = [{ id: MEMBER_ID }];

    render(<ScopedBusinessLink />);

    await waitFor(() => expect(mocks.setActive).toHaveBeenCalledWith(MEMBER_ID));
    expect(window.location.pathname + window.location.search).toBe('/integrations?connected=vk');
  });

  it.each([
    ['an inaccessible UUID', MEMBER_ID, []],
    ['a malformed value', 'https://evil.example/', [{ id: MEMBER_ID }]],
    [
      'a pending-deletion membership',
      MEMBER_ID,
      [{ id: MEMBER_ID, deletion_pending_until: '2026-09-10T00:00:00Z' }],
    ],
  ])('does not switch for %s', async (_case, requestedId, memberships) => {
    mocks.requestedId = requestedId;
    mocks.memberships = memberships;

    render(<ScopedBusinessLink />);

    await waitFor(() => expect(window.location.search).toBe('?connected=vk'));
    expect(mocks.setActive).not.toHaveBeenCalled();
  });
});
