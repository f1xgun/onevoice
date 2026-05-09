// Status vocabularies for the three list-view pages (posts / tasks / reviews).
//
// Each surface has its own state machine, so labels DON'T collapse to a
// single map — they're three separate exports. What gets centralized is
// the Russian copy + (for reviews) the tone mapping, so a brand-voice
// pass touches one file instead of three.

export type PostStatus = 'draft' | 'scheduled' | 'published' | 'error';

export const POST_STATUS_LABELS: Record<PostStatus, string> = {
  draft: 'Черновик',
  scheduled: 'Запланирован',
  published: 'Опубликован',
  error: 'Ошибка',
};

export type TaskStatus = 'pending' | 'running' | 'done' | 'error';

export const TASK_STATUS_LABELS: Record<TaskStatus, string> = {
  pending: 'Запланировано',
  running: 'В работе',
  done: 'Готово',
  error: 'Нужна помощь',
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
  pending: { label: 'Ждёт ответа', tone: 'warning' },
  replied: { label: 'Ответ отправлен', tone: 'success' },
  error: { label: 'Ошибка отправки', tone: 'danger' },
  read: { label: 'Прочитано', tone: 'neutral' },
};
