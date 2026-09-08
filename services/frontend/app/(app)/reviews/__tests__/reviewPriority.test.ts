import { describe, expect, it } from 'vitest';
import type { Review } from '@/types/review';
import { platformHasRating, sortLoadedReviewsLowRatingFirst } from '../_lib/reviewPriority';

function review(id: string, platform: string, rating: number): Review {
  return {
    id,
    businessId: 'business-a',
    platform,
    externalId: id,
    authorName: '',
    rating,
    text: '',
    replyStatus: 'pending',
    createdAt: '2026-09-09T00:00:00Z',
  };
}

describe('loaded review priority', () => {
  it('moves rated reviews at the existing <=3 boundary first and keeps stable order otherwise', () => {
    const sorted = sortLoadedReviewsLowRatingFirst([
      review('positive', 'google', 5),
      review('three', 'yandex_business', 3),
      review('one', '2gis', 1),
      review('four', 'google', 4),
    ]);
    expect(sorted.map(({ id }) => id)).toEqual(['one', 'three', 'positive', 'four']);
  });

  it('never treats rating-less platform rows as negative even when rating is zero', () => {
    expect(platformHasRating('telegram')).toBe(false);
    expect(platformHasRating('vk')).toBe(false);
    const sorted = sortLoadedReviewsLowRatingFirst([
      review('telegram', 'telegram', 0),
      review('google-negative', 'google', 3),
      review('vk', 'vk', 0),
    ]);
    expect(sorted.map(({ id }) => id)).toEqual(['google-negative', 'telegram', 'vk']);
  });
});
