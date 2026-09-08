import axios from 'axios';
import { createTranslator } from 'next-intl';
import { toast } from 'sonner';
import { API_BASE_URL, API_PATHS } from '@/lib/constants/apiPaths';
import { HTTP_STATUS } from '@/lib/constants/httpStatus';
import { useAuthStore } from './auth';
import { refreshAccessToken } from '@/lib/api/authFetch';
import { useBusinessStore } from '@/lib/stores/business';
import { queryClient } from '@/lib/queryClient';
import { BUSINESS_LIST_QUERY_KEY } from '@/lib/hooks/useBusinessList';
import { DEFAULT_LOCALE, isLocale, type Locale, LOCALE_COOKIE } from '@/lib/i18n/locales';

export const api = axios.create({
  baseURL: API_BASE_URL,
  headers: { 'Content-Type': 'application/json' },
  withCredentials: true,
});

// INVARIANT: this axios instance is CLIENT-ONLY.
//
// Server-rendered code paths (RSC, route handlers, server actions) MUST
// NOT route through `lib/api.ts` — they have no access to `document`,
// so `readClientLocale` would silently fall back to DEFAULT_LOCALE
// and ship the wrong user's locale to the backend. Server code should
// construct its own fetcher (or use the next-intl server APIs) and
// set `Accept-Language` from the incoming request's cookie / header.
//
// The SSR fallback below is defensive only. If it ever fires in
// production, that indicates a server-side caller wrongly imported
// this client module — fix the caller, not this fallback.

// Read the locale cookie on the client. Server callers (RSC, route
// handlers) hit this module only through code paths that don't reach
// `document` — those should call the backend via their own server-side
// fetcher. On the client we read it cookie-first so the value can't go
// stale between renders (React state would). Falls back to DEFAULT_LOCALE
// when running outside the browser or when the cookie isn't set yet.
function readClientLocale(): string {
  if (typeof document === 'undefined') return DEFAULT_LOCALE;
  const raw = document.cookie
    .split(';')
    .map((part) => part.trim())
    .find((part) => part.startsWith(`${LOCALE_COOKIE}=`));
  if (!raw) return DEFAULT_LOCALE;
  const value = decodeURIComponent(raw.slice(LOCALE_COOKIE.length + 1));
  return isLocale(value) ? value : DEFAULT_LOCALE;
}

// Capture telemetry scope before the request starts. Response interceptors may
// run after an organization switch, so the active store is no longer a safe
// source of attribution at that point. Non-business API URLs are explicitly
// global even while an organization is active.
function telemetryBusinessId(url: string | undefined): string | null {
  const encoded = /^\/businesses\/([^/?]+)(?:[/?]|$)/.exec(url ?? '')?.[1];
  if (!encoded) return null;
  try {
    return decodeURIComponent(encoded);
  } catch {
    return null;
  }
}

// Attach access token + Accept-Language to every request.
api.interceptors.request.use((config) => {
  const token = useAuthStore.getState().accessToken;
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  config.headers['Accept-Language'] = readClientLocale();
  config.metadata = {
    ...config.metadata,
    telemetryBusinessId: telemetryBusinessId(config.url),
  };
  return config;
});

// On 401: refresh once via the shared single-flight (refreshAccessToken,
// shared with authFetch so the refresh cookie is rotated exactly once),
// then replay the request. A failed refresh logs out + redirects to /login
// inside refreshAccessToken, so here we just reject.
api.interceptors.response.use(
  (res) => res,
  async (error) => {
    const original = error.config;

    const url = original?.url ?? '';

    if (error.response && !url.includes(API_PATHS.TELEMETRY)) {
      const correlationId = error.response.headers?.['x-correlation-id'] as string | undefined;
      if (correlationId) {
        import('./telemetry')
          .then(({ trackEvent }) => {
            trackEvent(
              'api_error',
              `${error.response.status} ${original?.method?.toUpperCase()} ${url}`,
              {
                correlationId,
                businessId: original?.metadata?.telemetryBusinessId ?? null,
                metadata: {
                  status: String(error.response.status),
                  url,
                },
              }
            );
          })
          .catch(() => {});
      }
    }

    const isAuthEndpoint =
      url.includes(API_PATHS.AUTH.LOGIN) ||
      url.includes(API_PATHS.AUTH.REGISTER) ||
      url.includes('/auth/refresh');

    if (
      error.response?.status !== HTTP_STATUS.UNAUTHORIZED ||
      original?._retry ||
      isAuthEndpoint ||
      url.includes(API_PATHS.TELEMETRY)
    ) {
      return Promise.reject(error);
    }

    original._retry = true;

    try {
      const accessToken = await refreshAccessToken();
      original.headers.Authorization = `Bearer ${accessToken}`;
      return api(original);
    } catch {
      return Promise.reject(error);
    }
  }
);

api.interceptors.response.use(
  (res) => res,
  async (error) => {
    const url = error.config?.url ?? '';
    const status = error.response?.status;
    const skipBusinessNotFound = error.config?.metadata?.skipBusinessNotFound === true;

    const businessId = /^\/businesses\/([^/?]+)(?:\/|$|\?)/.exec(url)?.[1];
    if (
      businessId &&
      businessId === useBusinessStore.getState().activeBusinessId &&
      status === HTTP_STATUS.NOT_FOUND &&
      url.startsWith('/businesses/') &&
      !skipBusinessNotFound
    ) {
      const { data: businesses } = await api
        .get<{ id: string }[]>('/businesses')
        .catch(() => ({ data: null }));
      if (
        !businesses ||
        businesses.some((business) => business.id === businessId) ||
        useBusinessStore.getState().activeBusinessId !== businessId
      )
        return Promise.reject(error);
      queryClient.setQueryData(BUSINESS_LIST_QUERY_KEY, businesses);
      useBusinessStore.getState().setActive(null);
      queryClient.invalidateQueries({ queryKey: BUSINESS_LIST_QUERY_KEY, exact: true });
      void showStaleBusinessToast();
    }

    return Promise.reject(error);
  }
);

async function showStaleBusinessToast() {
  const locale = readClientLocale() as Locale;
  try {
    const messages = (await import(`@/messages/${locale}.json`)).default;
    const t = createTranslator({ locale, messages, namespace: 'team.errors' });
    toast.warning(t('staleBusiness'));
  } catch {}
}
