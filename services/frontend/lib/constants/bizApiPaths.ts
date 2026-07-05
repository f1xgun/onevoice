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

import type { PlatformId } from '@/lib/platforms';

// Per-platform integration endpoints, keyed by PlatformId. Replaces the
// flat TELEGRAM_*/VK_*/GOOGLE_*/YANDEX_* fields. Each platform declares
// only the verbs it supports; absent platforms (e.g. 2gis, avito,
// whatsapp) read as `undefined` so call sites can branch off presence.
//
// The shape uses functions for paths that take an integration id and
// strings for static paths, matching the bizApiPaths convention.
export interface IntegrationPlatformEndpoints {
  connect?: string;
  refresh?: string;
  authUrl?: string;
  locations?: string;
  selectLocation?: string;
  probe?: string;
  companies?: string;
}

export const INTEGRATION_ENDPOINTS: Partial<Record<PlatformId, IntegrationPlatformEndpoints>> = {
  telegram: {
    connect: '/integrations/telegram/connect',
    refresh: '/integrations/telegram/refresh',
  },
  vk: {
    connect: '/integrations/vk/connect',
  },
  google_business: {
    authUrl: '/integrations/google_business/auth-url',
    locations: '/integrations/google_business/locations',
    selectLocation: '/integrations/google_business/select-location',
  },
  yandex_business: {
    connect: '/integrations/yandex_business/connect',
    probe: '/integrations/yandex_business/probe',
    companies: '/integrations/yandex_business/companies',
  },
};

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
  MEMBERS: {
    ROOT: '/members',
    BY_ID: (userId: string) => `/members/${userId}` as const,
  },
  INVITATIONS: {
    ROOT: '/invitations',
    BY_ID: (inviteId: string) => `/invitations/${inviteId}` as const,
  },
  ROLES: {
    ROOT: '/roles',
    BY_ID: (roleId: string) => `/roles/${roleId}` as const,
  },
  // Per-business "me" endpoints (actor's effective state).
  // BIZ_API_PATHS.ME.PERMISSIONS resolves to
  // GET /api/v1/businesses/{bizId}/me/permissions → { permissions: string[] }.
  ME: {
    PERMISSIONS: '/me/permissions',
  },
  INTEGRATIONS: {
    ROOT: '/integrations',
    BY_ID: (id: string) => `/integrations/${id}`,
  },
  REVIEWS: {
    ROOT: '/reviews',
    REFRESH: '/reviews/refresh',
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
  BILLING: {
    // GET /businesses/{id}/billing/summary → read-only usage transparency.
    SUMMARY: '/billing/summary',
  },
} as const;
