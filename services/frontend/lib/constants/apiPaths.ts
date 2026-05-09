// Single source of truth for API endpoint paths used by the frontend.
//
// Two flavours of paths live here:
//
// 1. Relative paths consumed by the axios instance in lib/api (which
//    prepends the base URL like "/api/v1"). These start without "/api/v1".
//    Example: API_PATHS.AUTH.LOGIN  →  '/auth/login'
//
// 2. Absolute paths used by the bare `fetch(...)` calls inside hooks that
//    need streaming (SSE) or custom headers — for those we keep the
//    "/api/v1" prefix inline. They live under API_PATHS.STREAM.
//
// Wave 2.2 will move "/api/v1" itself to NEXT_PUBLIC_API_URL; until then,
// the absolute paths below carry the literal prefix so a single env
// rollover stays scoped to one file.
//
// Functions vs strings:
//   - Static endpoints are exported as plain strings (`'/business'`).
//   - Endpoints that take an id (or other path param) are exported as
//     functions (`CONVERSATION_BY_ID(id)`) so call sites can't forget to
//     interpolate.

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
// refresh-token interceptor where wrapping would recurse). These keep
// the "/api/v1" prefix; Wave 2.2 will refactor it to env-driven.
export const API_STREAM_PATHS = {
  AUTH_REFRESH: '/api/v1/auth/refresh',
  TASKS_STREAM: '/api/v1/tasks/stream',
  CONVERSATION_MESSAGES: (id: string) => `/api/v1/conversations/${id}/messages`,
  CHAT: (id: string) => `/api/v1/chat/${id}`,
  PENDING_TOOL_CALLS_RESOLVE: (conversationId: string, batchId: string) =>
    `/api/v1/conversations/${conversationId}/pending-tool-calls/${batchId}/resolve`,
  CHAT_RESUME: (conversationId: string, batchId: string) =>
    `/api/v1/chat/${conversationId}/resume?batch_id=${batchId}`,
} as const;
