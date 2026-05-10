// Single source of truth for non-business-scoped API endpoint paths used
// by the frontend.
//
// For business-scoped endpoints (`/businesses/{id}/...`) see BIZ_API_PATHS
// in bizApiPaths.ts — those are consumed via the bizApi() helper which
// prepends `/businesses/{id}` automatically.
//
// Two flavours of paths live here:
//
// 1. Relative paths consumed by the axios instance in lib/api (which
//    prepends API_BASE_URL). These start without "/api/v1".
//    Example: API_PATHS.AUTH.LOGIN  →  '/auth/login'
//
// 2. Absolute paths used by the bare `fetch(...)` calls inside hooks that
//    need streaming (SSE) or custom headers. They prepend API_BASE_URL
//    explicitly. They live under API_STREAM_PATHS.
//
// API_BASE_URL is sourced from NEXT_PUBLIC_API_URL (build-time inlined by
// Next.js). Default is "/api/v1" so the existing same-origin rewrite proxy
// in next.config.js (source: /api/v1/:path*) keeps working unchanged.
// Production deploys serving the frontend from a different origin can set
// NEXT_PUBLIC_API_URL=https://api.example.com/api/v1 to bypass the rewrite.
//
// Functions vs strings:
//   - Static endpoints are exported as plain strings (`'/auth/login'`).
//   - Endpoints that take an id (or other path param) are exported as
//     functions so call sites can't forget to interpolate.
//
// Some entries below double as frontend Next.js route hrefs (e.g.
// BUSINESS.ROOT, INTEGRATIONS.ROOT, TASKS) because the URL slugs match
// the API paths one-to-one. Splitting them into a FRONTEND_ROUTES module
// is out of scope for the RBAC migration — see PR backlog.

export const API_BASE_URL: string = process.env.NEXT_PUBLIC_API_URL || '/api/v1';

export const API_PATHS = {
  AUTH: {
    LOGIN: '/auth/login',
    REGISTER: '/auth/register',
    PASSWORD: '/auth/password',
    ME: '/auth/me',
  },
  // BUSINESS.ROOT also doubles as the frontend route href for the
  // business profile page. The /business API endpoint itself moved under
  // BIZ_API_PATHS.BUSINESS — see bizApiPaths.ts.
  BUSINESS: {
    ROOT: '/business',
  },
  // INTEGRATIONS.ROOT also doubles as the frontend route href. All API
  // calls under /integrations migrated to BIZ_API_PATHS.INTEGRATIONS.
  INTEGRATIONS: {
    ROOT: '/integrations',
  },
  // TASKS doubles as the frontend route href. Backend list/stream moved
  // to BIZ_API_PATHS.TASKS.
  TASKS: '/tasks',
  TELEMETRY: '/telemetry',
  // POST /reviews/refresh is INTENTIONALLY NOT business-scoped — it
  // synchronously fans out a sync request to every connected agent for
  // the caller's businesses. Per-business calls live under
  // BIZ_API_PATHS.REVIEWS.
  REVIEWS: {
    REFRESH: '/reviews/refresh',
  },
} as const;

// Absolute paths used by bare `fetch(...)` / `axios(...)` calls that
// bypass the wrapped api instance (SSE streaming, HITL resume, and the
// refresh-token interceptor where wrapping would recurse). They prepend
// API_BASE_URL explicitly so NEXT_PUBLIC_API_URL applies uniformly.
export const API_STREAM_PATHS = {
  AUTH_REFRESH: `${API_BASE_URL}/auth/refresh`,
  TASKS_STREAM: `${API_BASE_URL}/tasks/stream`,
  CONVERSATION_MESSAGES: (id: string) => `${API_BASE_URL}/conversations/${id}/messages`,
  CHAT: (id: string) => `${API_BASE_URL}/chat/${id}`,
  PENDING_TOOL_CALLS_RESOLVE: (conversationId: string, batchId: string) =>
    `${API_BASE_URL}/conversations/${conversationId}/pending-tool-calls/${batchId}/resolve`,
  CHAT_RESUME: (conversationId: string, batchId: string) =>
    `${API_BASE_URL}/chat/${conversationId}/resume?batch_id=${batchId}`,
};
