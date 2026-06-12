// Non-business-scoped API endpoint paths. Business-scoped endpoints
// (`/businesses/{id}/...`) live in bizApiPaths.ts and flow through bizApi().
//
// API_PATHS = relative paths consumed by the wrapped axios instance.
// API_STREAM_PATHS = absolute paths used by bare fetch() (SSE, refresh
// interceptor); prepend API_BASE_URL explicitly.
//
// API_BASE_URL is build-time inlined from NEXT_PUBLIC_API_URL. The default
// "/api/v1" keeps the next.config.js same-origin rewrite working; cross-
// origin deploys can point it at an absolute URL.
//
// Param-taking endpoints are exported as functions so call sites can't
// forget to interpolate.

export const API_BASE_URL: string = process.env.NEXT_PUBLIC_API_URL || '/api/v1';

export const API_PATHS = {
  AUTH: {
    LOGIN: '/auth/login',
    REGISTER: '/auth/register',
    PASSWORD: '/auth/password',
    PROFILE: '/auth/profile',
    ME: '/auth/me',
    VERIFY_EMAIL_RESEND: '/auth/verify-email/resend',
    EMAIL_BEFORE_VERIFY: '/auth/email-before-verify',
  },
  // BUSINESS / INTEGRATIONS / TASKS also double as frontend route hrefs.
  // The API endpoints themselves moved under BIZ_API_PATHS.
  BUSINESS: {
    ROOT: '/business',
  },
  INTEGRATIONS: {
    ROOT: '/integrations',
  },
  TASKS: '/tasks',
  TELEMETRY: '/telemetry',
  // Public invitation routes go through raw `api` (NOT `bizApi`). Preview
  // call must pass `skipBusinessNotFound: true` so the 404 interceptor
  // doesn't mistake a missing token for a stale active business.
  INVITATIONS_PUBLIC: {
    PREVIEW: (token: string) => `/invitations/${token}` as const,
    ACCEPT: (token: string) => `/invitations/${token}/accept` as const,
  },
  // App-static permission catalog (NOT business-scoped).
  PERMISSIONS: '/permissions',
} as const;

// Absolute paths used by bare `fetch(...)` / `axios(...)` calls that
// bypass the wrapped api instance (SSE streaming, HITL resume, refresh
// interceptor where wrapping would recurse).
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
