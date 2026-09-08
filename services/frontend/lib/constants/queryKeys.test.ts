import { describe, expect, it } from 'vitest';
import { QUERY_KEYS } from './queryKeys';

describe('review SLA query key', () => {
  it('is business-scoped and nested under the review invalidation prefix', () => {
    const businessA = QUERY_KEYS.BUSINESS_REVIEW_SLA('business-a');
    const businessB = QUERY_KEYS.BUSINESS_REVIEW_SLA('business-b');
    expect(businessA).not.toEqual(businessB);
    expect(businessA.slice(0, 3)).toEqual(QUERY_KEYS.BUSINESS_REVIEWS('business-a'));
  });
});
