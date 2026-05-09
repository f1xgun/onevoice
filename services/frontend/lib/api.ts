import axios from 'axios';
import { API_BASE_URL, API_PATHS, API_STREAM_PATHS } from '@/lib/constants/apiPaths';
import { HTTP_STATUS } from '@/lib/constants/httpStatus';
import { useAuthStore } from './auth';
import type { User } from './auth';
import { useBusinessStore } from '@/lib/stores/business';
import { queryClient } from '@/lib/queryClient';
import { BUSINESS_LIST_QUERY_KEY } from '@/lib/hooks/useBusinessList';

export const api = axios.create({
  baseURL: API_BASE_URL,
  headers: { 'Content-Type': 'application/json' },
  withCredentials: true,
});

// Attach access token to every request
api.interceptors.request.use((config) => {
  const token = useAuthStore.getState().accessToken;
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
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

    // Don't intercept 401 from auth endpoints — let the caller handle it
    const url = original?.url ?? '';

    // Track API errors with correlation ID (skip telemetry endpoint to prevent loops)
    if (error.response && !url.includes(API_PATHS.TELEMETRY)) {
      const correlationId = error.response.headers?.['x-correlation-id'] as string | undefined;
      if (correlationId) {
        // Lazy import to avoid circular dependency (telemetry.ts imports api)
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
          .catch(() => {
            // Silently ignore — telemetry must never break the app
          });
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

// 404 interceptor (D-16): on /businesses/{id}/... 404, the active business
// is stale (server-side membership removed). Clear the store + re-fetch
// the list. Phase 4 layers a switcher-redirect UX on top of this.
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
    }

    return Promise.reject(error);
  }
);
