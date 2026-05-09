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
  // Business-scoped keys (RBAC: cache must be partitioned per active business).
  // bizId is `string | null` — matches `useBusinessStore.activeBusinessId`,
  // which is null until a business is selected. Queries that depend on it
  // gate fetching with `enabled: !!activeBusinessId` so a null-keyed cache
  // entry never resolves data; we keep the param nullable so callers can
  // forward the store value verbatim without ad-hoc non-null assertions.
  BUSINESS_INTEGRATIONS: (bizId: string | null) => ['businesses', bizId, 'integrations'] as const,
  BUSINESS_PROFILE: (bizId: string | null) => ['businesses', bizId, 'business'] as const,
  BUSINESS_REVIEWS: (bizId: string | null) => ['businesses', bizId, 'reviews'] as const,
  BUSINESS_TASKS: (bizId: string | null) => ['businesses', bizId, 'tasks'] as const,
  REVIEWS_FILTERED: (platform: string, replyStatus: string) =>
    ['reviews', platform, replyStatus] as const,
  POSTS: (status: string, platform: string) => ['posts', status, platform] as const,
  PROJECT_CONVERSATION_COUNT: (id: string) => ['projects', id, 'conversation-count'] as const,
} as const;
