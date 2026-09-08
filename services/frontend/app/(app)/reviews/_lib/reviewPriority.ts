import type { Review } from '@/types/review';

const platformsWithRating = new Set([
  'yandex_business',
  'yandex',
  'google',
  'google_business',
  '2gis',
]);

export function platformHasRating(id: string): boolean {
  return platformsWithRating.has(id);
}

// This only reorders the reviews already returned by the paginated endpoint.
// Rating-less platforms keep their relative position in the non-negative group.
export function sortLoadedReviewsLowRatingFirst(reviews: Review[]): Review[] {
  return reviews
    .map((review, index) => ({ review, index }))
    .sort((left, right) => {
      const leftLow = platformHasRating(left.review.platform) && left.review.rating <= 3;
      const rightLow = platformHasRating(right.review.platform) && right.review.rating <= 3;
      if (leftLow !== rightLow) return leftLow ? -1 : 1;
      if (leftLow && rightLow && left.review.rating !== right.review.rating) {
        return left.review.rating - right.review.rating;
      }
      return left.index - right.index;
    })
    .map(({ review }) => review);
}
