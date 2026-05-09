// Single source of truth for business-scoped API endpoint paths.
//
// Paths here are RELATIVE to the `/businesses/{bizId}` prefix that
// `bizApi(bizId)` (lib/api/business-api.ts) prepends automatically.
// Every entry must therefore start with `/` (or be the empty string for
// the root — `bizApi(bizId).put('', body)` hits PUT /businesses/{bizId}).
//
// Two flavours, mirroring the convention in apiPaths.ts:
//   - Static endpoints are exported as plain strings (`'/integrations'`).
//   - Endpoints that take a path param are exported as functions
//     (`BY_ID(id)`) so call sites can't forget to interpolate.
//
// For non-business-scoped endpoints (auth, refresh, public platforms,
// telemetry, the cross-business POST /reviews/refresh) see
// API_PATHS / API_STREAM_PATHS in apiPaths.ts.

export const BIZ_API_PATHS = {
  // Root business document — bizApi(id).get('') → GET /businesses/{id}.
  // The empty string is intentional: bizApi already adds the prefix and
  // the API route has no trailing slash.
  BUSINESS: {
    ROOT: '',
    LOGO: '/logo',
    SCHEDULE: '/schedule',
    VOICE_TONE: '/voice-tone',
  },
  CONVERSATIONS: {
    ROOT: '/conversations',
    BY_ID: (id: string) => `/conversations/${id}`,
    PIN: (id: string) => `/conversations/${id}/pin`,
    UNPIN: (id: string) => `/conversations/${id}/unpin`,
    MOVE: (id: string) => `/conversations/${id}/move`,
    REGENERATE_TITLE: (id: string) => `/conversations/${id}/regenerate-title`,
  },
  INTEGRATIONS: {
    ROOT: '/integrations',
    BY_ID: (id: string) => `/integrations/${id}`,
    TELEGRAM_CONNECT: '/integrations/telegram/connect',
    TELEGRAM_REFRESH: '/integrations/telegram/refresh',
    VK_CONNECT: '/integrations/vk/connect',
    VK_REFRESH_NAME: (id: string) => `/integrations/vk/${id}/refresh-name`,
    GOOGLE_AUTH_URL: '/integrations/google_business/auth-url',
    GOOGLE_LOCATIONS: '/integrations/google_business/locations',
    GOOGLE_SELECT_LOCATION: '/integrations/google_business/select-location',
    YANDEX_BUSINESS_PROBE: '/integrations/yandex_business/probe',
    YANDEX_BUSINESS_COMPANIES: '/integrations/yandex_business/companies',
    YANDEX_BUSINESS_CONNECT: '/integrations/yandex_business/connect',
    YANDEX_BUSINESS_REFRESH_NAME: (id: string) =>
      `/integrations/yandex_business/${id}/refresh-name`,
  },
  REVIEWS: {
    ROOT: '/reviews',
    REPLY: (id: string) => `/reviews/${id}/reply`,
  },
  POSTS: {
    ROOT: '/posts',
  },
  TASKS: {
    ROOT: '/tasks',
    STREAM: '/tasks/stream',
  },
  TOOLS: {
    ROOT: '/tools',
  },
  TOOL_APPROVALS: {
    ROOT: '/tool-approvals',
  },
  PROJECTS: {
    ROOT: '/projects',
    BY_ID: (id: string) => `/projects/${id}`,
    CONVERSATION_COUNT: (id: string) => `/projects/${id}/conversation-count`,
  },
  SEARCH: {
    ROOT: '/search',
  },
} as const;
