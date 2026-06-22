'use client';

import { useMutation, type UseMutationResult } from '@tanstack/react-query';

import { submitFeedback, type FeedbackInput } from '@/lib/api/feedback';

// useSubmitFeedback posts an in-app feedback submission. There is nothing to
// invalidate; the dialog toasts on success/error from the component.
export function useSubmitFeedback(): UseMutationResult<void, Error, FeedbackInput> {
  return useMutation<void, Error, FeedbackInput>({
    mutationFn: (input) => submitFeedback(input),
  });
}
