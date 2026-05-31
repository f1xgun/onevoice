// Status vocabularies for the three list-view pages (posts / tasks / reviews).
// Each surface has its own state machine — labels do NOT collapse to a single
// map. Copy lives in messages/*.json; the tone mapping for reviews stays
// here because `<Badge tone>` is a TS-side prop union, not translatable copy.
//
// Each hook memoizes its record on translator identity so callers can safely
// pass it into dependency arrays.

import { useMemo } from 'react';
import { useTranslations } from 'next-intl';

export type PostStatus = 'draft' | 'scheduled' | 'published' | 'error';

export function usePostStatusLabels(): Record<PostStatus, string> {
  const tPost = useTranslations('posts.status');
  return useMemo(
    () => ({
      draft: tPost('draft'),
      scheduled: tPost('scheduled'),
      published: tPost('published'),
      error: tPost('error'),
    }),
    [tPost]
  );
}

export type TaskStatus = 'pending' | 'running' | 'done' | 'error';

export function useTaskStatusLabels(): Record<TaskStatus, string> {
  const tTask = useTranslations('tasks.status');
  return useMemo(
    () => ({
      pending: tTask('pending'),
      running: tTask('running'),
      done: tTask('done'),
      error: tTask('error'),
    }),
    [tTask]
  );
}

// Tailwind background classes for the status dot in the tasks list.
// Locale-invariant — kept alongside the labels because the dot+label
// pair appears together at every render site.
export const TASK_STATUS_DOT_CLASSES: Record<TaskStatus, string> = {
  pending: 'bg-ink-faint',
  running: 'bg-ochre',
  done: 'bg-success',
  error: 'bg-danger',
};

export type ReviewStatus = 'pending' | 'replied' | 'error' | 'read';
export type ReviewBadgeTone = 'success' | 'warning' | 'danger' | 'neutral';
export interface ReviewBadge {
  label: string;
  tone: ReviewBadgeTone;
}

// Tone names match the <Badge tone=...> prop literal union. The label
// half is per brand-voice guide §3: matter-of-fact, not celebratory.
export function useReviewStatusBadges(): Record<ReviewStatus, ReviewBadge> {
  const tReview = useTranslations('reviews.status');
  return useMemo(
    () => ({
      pending: { label: tReview('pending'), tone: 'warning' },
      replied: { label: tReview('replied'), tone: 'success' },
      error: { label: tReview('error'), tone: 'danger' },
      read: { label: tReview('read'), tone: 'neutral' },
    }),
    [tReview]
  );
}
