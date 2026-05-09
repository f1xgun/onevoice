// Status vocabularies for the three list-view pages (posts / tasks / reviews).
//
// Each surface has its own state machine, so labels DON'T collapse to a
// single map — they're three separate exports. The Russian copy lives in
// `messages/ru.json` under `posts.status.*`, `tasks.status.*`, and
// `reviews.status.*`; the tone mapping for reviews stays here because the
// `<Badge tone>` prop union is a TS-side concern, not translatable copy.

import { getTranslator } from '@/lib/i18n/translator';

const tPost = getTranslator('posts.status');
const tTask = getTranslator('tasks.status');
const tReview = getTranslator('reviews.status');

export type PostStatus = 'draft' | 'scheduled' | 'published' | 'error';

export const POST_STATUS_LABELS: Record<PostStatus, string> = {
  draft: tPost('draft'),
  scheduled: tPost('scheduled'),
  published: tPost('published'),
  error: tPost('error'),
};

export type TaskStatus = 'pending' | 'running' | 'done' | 'error';

export const TASK_STATUS_LABELS: Record<TaskStatus, string> = {
  pending: tTask('pending'),
  running: tTask('running'),
  done: tTask('done'),
  error: tTask('error'),
};

// Tailwind background classes for the status dot in the tasks list.
// Kept alongside the labels because the dot+label pair appears together
// at every render site.
export const TASK_STATUS_DOT_CLASSES: Record<TaskStatus, string> = {
  pending: 'bg-ink-faint',
  running: 'bg-ochre',
  done: 'bg-success',
  error: 'bg-danger',
};

export type ReviewStatus = 'pending' | 'replied' | 'error' | 'read';

// Per brand-voice guide §3 the labels are matter-of-fact, not celebratory.
// Tone names match the <Badge tone=...> prop literal union.
export const REVIEW_STATUS_BADGES: Record<
  ReviewStatus,
  { label: string; tone: 'success' | 'warning' | 'danger' | 'neutral' }
> = {
  pending: { label: tReview('pending'), tone: 'warning' },
  replied: { label: tReview('replied'), tone: 'success' },
  error: { label: tReview('error'), tone: 'danger' },
  read: { label: tReview('read'), tone: 'neutral' },
};
