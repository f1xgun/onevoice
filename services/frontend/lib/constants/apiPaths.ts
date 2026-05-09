// Single source of truth for API endpoint paths used by the frontend.
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
//   - Static endpoints are exported as plain strings (`'/business'`).
//   - Endpoints that take an id (or other path param) are exported as
//     functions (`CONVERSATION_BY_ID(id)`) so call sites can't forget to
//     interpolate.

export const API_BASE_URL: string = process.env.NEXT_PUBLIC_API_URL || '/api/v1';

export const API_PATHS = {
  AUTH: {
    LOGIN: '/auth/login',
    REGISTER: '/auth/register',
    PASSWORD: '/auth/password',
    ME: '/auth/me',
  },
  BUSINESS: {
    ROOT: '/business',
    SCHEDULE: '/business/schedule',
    VOICE_TONE: '/business/voice-tone',
  },
  CONVERSATIONS: {
    ROOT: '/conversations',
    BY_ID: (id: string) => `/conversations/${id}`,
    MOVE: (id: string) => `/conversations/${id}/move`,
    PIN: (id: string) => `/conversations/${id}/pin`,
    UNPIN: (id: string) => `/conversations/${id}/unpin`,
    REGENERATE_TITLE: (id: string) => `/conversations/${id}/regenerate-title`,
  },
  INTEGRATIONS: {
    ROOT: '/integrations',
    TELEGRAM_CONNECT: '/integrations/telegram/connect',
    YANDEX_BUSINESS_CONNECT: '/integrations/yandex_business/connect',
    GOOGLE_AUTH_URL: '/integrations/google_business/auth-url',
    GOOGLE_LOCATIONS: '/integrations/google_business/locations',
    GOOGLE_SELECT_LOCATION: '/integrations/google_business/select-location',
  },
  POSTS: '/posts',
  TASKS: '/tasks',
  TELEMETRY: '/telemetry',
  REVIEWS: {
    ROOT: '/reviews',
    REFRESH: '/reviews/refresh',
    REPLY: (id: string) => `/reviews/${id}/reply`,
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
