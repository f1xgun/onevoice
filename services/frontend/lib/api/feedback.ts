import { api } from '@/lib/api';
import { API_PATHS } from '@/lib/constants/apiPaths';

export type FeedbackCategory = 'bug' | 'idea' | 'question' | 'other';

export interface FeedbackInput {
  category: FeedbackCategory;
  message: string;
  page?: string;
  rating?: number;
}

// submitFeedback POSTs an in-app feedback submission. The endpoint returns
// 204 No Content; the caller toasts on resolve.
export async function submitFeedback(input: FeedbackInput): Promise<void> {
  const body: Record<string, unknown> = {
    category: input.category,
    message: input.message,
  };
  if (input.page) {
    body.page = input.page;
  }
  if (typeof input.rating === 'number') {
    body.rating = input.rating;
  }
  await api.post(API_PATHS.FEEDBACK, body);
}
