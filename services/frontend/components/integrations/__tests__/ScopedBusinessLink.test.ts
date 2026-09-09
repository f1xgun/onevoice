import { describe, expect, it } from 'vitest';

import { resolveScopedBusinessId } from '../ScopedBusinessLink';

const VALID_ID = '7b7d23dc-d21c-4e01-9f10-1f4bea92a1bf';

describe('resolveScopedBusinessId', () => {
  it('accepts an active organization in the membership list', () => {
    expect(resolveScopedBusinessId(VALID_ID, [{ id: VALID_ID }])).toBe(VALID_ID);
  });

  it('rejects an organization outside the membership list', () => {
    expect(resolveScopedBusinessId(VALID_ID, [])).toBeNull();
  });

  it('rejects malformed and pending-deletion organization IDs', () => {
    expect(resolveScopedBusinessId('https://example.com', [{ id: VALID_ID }])).toBeNull();
    expect(
      resolveScopedBusinessId(VALID_ID, [
        { id: VALID_ID, deletion_pending_until: '2026-09-10T00:00:00Z' },
      ])
    ).toBeNull();
  });
});
