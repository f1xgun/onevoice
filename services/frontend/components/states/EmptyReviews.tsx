// components/states/EmptyReviews.tsx — empty state for /reviews.
//
// Mock anchor: design_handoff_onevoice 2/mocks/mock-states.jsx
// "Нет отзывов за период" (lines 137–146). The page already shipped
// with a tailored empty state for filter combos — this component
// covers the canonical "no reviews this period" case and the two
// frequent filter cases (pending only / replied only) so callers can
// pick a copy preset without reinventing the layout.

'use client';

import Link from 'next/link';
import { useTranslations } from 'next-intl';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { EmptyFrame } from './EmptyFrame';

export type ReviewsEmptyMode = 'all' | 'pending' | 'replied';

export interface EmptyReviewsProps {
  mode?: ReviewsEmptyMode;
  onResetFilters?: () => void;
  /** Optional pivot — "look at last week" link. */
  onLookBack?: () => void;
}

export function EmptyReviews({ mode = 'all', onLookBack, onResetFilters }: EmptyReviewsProps) {
  const tStates = useTranslations('states.emptyReviews');
  const title =
    onResetFilters && mode === 'all'
      ? tStates('filteredTitle')
      : tStates(`${mode}.title` as `${ReviewsEmptyMode}.title`);
  const body =
    onResetFilters && mode === 'all'
      ? tStates('filteredBody')
      : tStates(`${mode}.body` as `${ReviewsEmptyMode}.body`);
  return (
    <EmptyFrame
      compact
      title={title}
      body={body}
      action={
        onResetFilters ? (
          <Button onClick={onResetFilters}>{tStates('resetFilters')}</Button>
        ) : onLookBack && mode === 'all' ? (
          <Button variant="ghost" size="sm" onClick={onLookBack}>
            {tStates('lookBack')}
          </Button>
        ) : (
          <Button asChild>
            <Link href="/integrations">{tStates('connectChannels')}</Link>
          </Button>
        )
      }
    />
  );
}
