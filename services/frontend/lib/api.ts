import axios from 'axios';
import { createTranslator } from 'next-intl';
import { toast } from 'sonner';
import { API_BASE_URL, API_PATHS, API_STREAM_PATHS } from '@/lib/constants/apiPaths';
import { HTTP_STATUS } from '@/lib/constants/httpStatus';
import { useAuthStore } from './auth';
import type { User } from './auth';
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

// Attach access token + Accept-Language to every request.
api.interceptors.request.use((config) => {
  const token = useAuthStore.getState().accessToken;
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  config.headers['Accept-Language'] = readClientLocale();
  return config;
});

// On 401: try refresh once, then logout
let refreshing = false;
let queue: Array<{ resolve: (v: string) => void; reject: (e: unknown) => void }> = [];

interface RefreshResponse {
  user: User;
  accessToken: string;
}

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

    if (error.response?.status !== HTTP_STATUS.UNAUTHORIZED || original._retry || isAuthEndpoint) {
      return Promise.reject(error);
    }

    if (refreshing) {
      return new Promise((resolve, reject) => {
        queue.push({ resolve, reject });
      }).then((token) => {
        original.headers.Authorization = `Bearer ${token}`;
        return api(original);
      });
    }

    original._retry = true;
    refreshing = true;

    try {
      const { data } = await axios.post<RefreshResponse>(
        API_STREAM_PATHS.AUTH_REFRESH,
        {},
        { withCredentials: true }
      );
      if (!data.accessToken) throw new Error('invalid refresh response');
      const { accessToken, user } = data;

      if (user) {
        useAuthStore.getState().setAuth(user, accessToken);
      } else {
        useAuthStore.getState().setAccessToken(accessToken);
      }

      queue.forEach(({ resolve }) => resolve(accessToken));
      queue = [];

      original.headers.Authorization = `Bearer ${accessToken}`;
      return api(original);
    } catch {
      queue.forEach(({ reject }) => reject(error));
      queue = [];
      useAuthStore.getState().logout();
      window.location.href = '/login';
      return Promise.reject(error);
    } finally {
      refreshing = false;
    }
  }
);

// 404 interceptor: on /businesses/{id}/.. 404, the active business
// is stale (server-side membership removed). Clear the store + re-fetch
// the list + show a sonner warning toast. The redirect to /onboarding or
// /chat happens implicitly via BusinessRequiredGuard on the next render
// (RESEARCH P-04: do not call router from a module-level interceptor).
api.interceptors.response.use(
  (res) => res,
  async (error) => {
    const url = error.config?.url ?? '';
    const status = error.response?.status;
    const skipBusinessNotFound = error.config?.metadata?.skipBusinessNotFound === true;

    if (
      status === HTTP_STATUS.NOT_FOUND &&
      url.startsWith('/businesses/') &&
      !skipBusinessNotFound
    ) {
      useBusinessStore.getState().clear();
      queryClient.invalidateQueries({ queryKey: BUSINESS_LIST_QUERY_KEY });
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
