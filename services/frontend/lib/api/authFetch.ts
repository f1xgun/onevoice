// Shared client-side fetch + access-token refresh plumbing.
//
// The axios `api` instance (lib/api.ts) already attaches the access token
// and refreshes it once on a 401. But streaming endpoints (chat SSE, tasks
// SSE) and a handful of raw-fetch JSON clients can't go through axios —
// they need the raw `Response` to read `response.body`, or to inspect the
// status code directly. Those used to call `fetch` with a hand-written
// `Authorization: Bearer ${token}` header, which bypassed the refresh
// interceptor entirely: once the 15-minute access-token TTL elapsed (e.g.
// the tab sat idle), every such call 401'd and silently failed until a
// full page reload re-ran the layout-mount refresh.
//
// `authFetch` gives those callers the same behavior as axios: attach the
// in-memory access token, and on a 401 transparently refresh the token
// once and replay the request. Crucially, both axios and authFetch share
// ONE in-flight refresh (`refreshAccessToken`), so a token expiry that
// fires a JSON call and an SSE reconnect at the same instant rotates the
// refresh cookie exactly once instead of racing two refreshes into a
// spurious logout.

import axios from 'axios';
import { loginRedirectPath } from '@/lib/postAuthRedirect';
import { API_STREAM_PATHS } from '@/lib/constants/apiPaths';
import { HTTP_STATUS } from '@/lib/constants/httpStatus';
import { useAuthStore } from '@/lib/auth';
import type { User } from '@/lib/auth';

interface RefreshResponse {
  user?: User;
  accessToken: string;
}

let inFlightRefresh: Promise<string> | null = null;

// refreshAccessToken POSTs /auth/refresh and updates the auth store with the
// new access token. Concurrent callers share the single in-flight request,
// so the refresh cookie is rotated exactly once. It always rejects on
// failure so callers can stop their own work (abort an SSE reconnect loop,
// surface an error), but only logs the user out + redirects to /login when
// the failure is session-terminal — a 401 from /auth/refresh, i.e. the
// refresh cookie itself is invalid. A transient failure (network flap, or
// the backend's 500 on a Redis/DB blip) must NOT log the user out of an
// otherwise-valid session: the caller fails quietly and retries (e.g. the
// tasks stream reconnects with backoff).
export function refreshAccessToken(): Promise<string> {
  if (inFlightRefresh) return inFlightRefresh;

  inFlightRefresh = (async () => {
    try {
      const { data } = await axios.post<RefreshResponse>(
        API_STREAM_PATHS.AUTH_REFRESH,
        {},
        { withCredentials: true }
      );
      if (!data.accessToken) throw new Error('invalid refresh response');
      if (data.user) {
        useAuthStore.getState().setAuth(data.user, data.accessToken);
      } else {
        useAuthStore.getState().setAccessToken(data.accessToken);
      }
      return data.accessToken;
    } catch (err) {
      const status = (err as { response?: { status?: number } })?.response?.status;
      if (status === HTTP_STATUS.UNAUTHORIZED) {
        useAuthStore.getState().logout();
        if (typeof window !== 'undefined' && !window.location.pathname.startsWith('/login')) {
          window.location.href = loginRedirectPath(window.location);
        }
      }
      throw err;
    } finally {
      inFlightRefresh = null;
    }
  })();

  return inFlightRefresh;
}

function withAuth(init: RequestInit, token: string | null): RequestInit {
  const headers = new Headers(init.headers);
  if (token) headers.set('Authorization', `Bearer ${token}`);
  return { ...init, headers };
}

// authFetch is `fetch` with the in-memory access token attached and a
// transparent one-shot 401 refresh+retry. It returns the raw Response so
// SSE callers can read `response.body`. On a 401 it refreshes the token
// once (shared single-flight) and replays the request with the same
// method/body/signal; any other status — including a second 401 after the
// retry — is returned to the caller unchanged.
export async function authFetch(input: string, init: RequestInit = {}): Promise<Response> {
  const token = useAuthStore.getState().accessToken;
  const res = await fetch(input, withAuth(init, token));
  if (res.status !== HTTP_STATUS.UNAUTHORIZED) return res;

  let nextToken: string;
  try {
    nextToken = await refreshAccessToken();
  } catch {
    return res;
  }
  return fetch(input, withAuth(init, nextToken));
}
