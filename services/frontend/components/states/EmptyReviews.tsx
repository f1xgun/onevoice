// components/states/EmptyReviews.tsx — empty state for /reviews.
//
// Mock anchor: design_handoff_onevoice 2/mocks/mock-states.jsx
// "Нет отзывов за период" (lines 137–146). The page already shipped
// with a tailored empty state for filter combos — this component
// covers the canonical "no reviews this period" case and the two
// frequent filter cases (pending only / replied only) so callers can
// pick a copy preset without reinventing the layout.

import * as React from 'react';
import { Button } from '@/components/ui/button';
import { EmptyFrame } from './EmptyFrame';

export type ReviewsEmptyMode = 'all' | 'pending' | 'replied';

export interface EmptyReviewsProps {
  mode?: ReviewsEmptyMode;
  /** Optional pivot — "look at last week" link. */
  onLookBack?: () => void;
}

const COPY: Record<ReviewsEmptyMode, { title: string; body: string }> = {
  all: {
    title: 'За эту неделю отзывов не было',
    body: 'Новые отзывы появятся, когда клиенты их оставят.',
  },
  pending: {
    title: 'Открытых отзывов нет',
    body: 'Все обращения обработаны. OneVoice сообщит, как только придёт новый.',
  },
  replied: {
    title: 'Пока нечего показать',
    body: 'Отвеченных отзывов в выборке нет. Снимите фильтр, чтобы увидеть остальные.',
  },
};

export function EmptyReviews({ mode = 'all', onLookBack }: EmptyReviewsProps) {
  const { title, body } = COPY[mode];
  return (
    <EmptyFrame
      compact
      title={title}
      body={body}
      action={
        onLookBack && mode === 'all' ? (
          <Button variant="ghost" size="sm" onClick={onLookBack}>
            Прошлая неделя →
          </Button>
        ) : undefined
      }
    />
  );
}
