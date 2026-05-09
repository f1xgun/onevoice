// React Query keys for the frontend.
//
// Static keys are exported as `as const`-marked tuples so TypeScript
// infers their literal types — that's what react-query needs to build
// stable cache scopes across remounts.
//
// Keys with discriminator parameters (id, status, filter) are exported
// as factory functions that return `as const` tuples for the same reason.
//
// Existing per-hook keys (e.g. PLATFORMS_QUERY_KEY in lib/hooks/usePlatforms,
// TOOLS_QUERY_KEY) live next to their hooks and continue to do so —
// this file collects only the keys that pages used inline before this PR.

export const QUERY_KEYS = {
  CONVERSATIONS: ['conversations'] as const,
  CONVERSATION_BY_ID: (id: string) => ['conversations', id] as const,
  INTEGRATIONS: ['integrations'] as const,
  BUSINESS: ['business'] as const,
  TASKS: ['tasks'] as const,
  REVIEWS: ['reviews'] as const,
  REVIEWS_FILTERED: (platform: string, replyStatus: string) =>
    ['reviews', platform, replyStatus] as const,
  POSTS: (status: string, platform: string) => ['posts', status, platform] as const,
  PROJECT_CONVERSATION_COUNT: (id: string) => ['projects', id, 'conversation-count'] as const,
} as const;
