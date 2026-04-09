import axios from 'axios';
import { useAuthStore } from './auth';
import type { User } from './auth';

export const api = axios.create({
  baseURL: '/api/v1',
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
    if (error.response && !url.includes('/telemetry')) {
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
      url.includes('/auth/login') ||
      url.includes('/auth/register') ||
      url.includes('/auth/refresh');

    if (error.response?.status !== 401 || original._retry || isAuthEndpoint) {
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
        '/api/v1/auth/refresh',
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
