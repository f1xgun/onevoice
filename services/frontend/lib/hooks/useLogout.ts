import { api } from '@/lib/api';
import { useAuthStore } from '@/lib/auth';
import { queryClient } from '@/lib/queryClient';
import { API_PATHS } from '@/lib/constants/apiPaths';

/**
 * Returns a logout function that revokes the server session before clearing
 * local state. The order is load-bearing: `POST /auth/logout` sits behind the
 * Auth middleware and needs the in-memory access token in the Authorization
 * header, so it must run BEFORE the in-memory store is cleared. The backend
 * deletes the refresh token from Redis, clears the `__Host-refresh_token`
 * httpOnly cookie, and writes the logout audit entry; skipping it leaves a
 * valid refresh cookie that silently re-authenticates the next user on a
 * shared device into the previous tenant.
 *
 * The POST error is swallowed so a backend hiccup still clears the client.
 * `queryClient.clear()` wipes every tenant-scoped cache, not just a subset.
 */
export function useLogout(): () => Promise<void> {
  return async () => {
    await api.post(API_PATHS.AUTH.LOGOUT).catch(() => undefined);
    queryClient.clear();
    useAuthStore.getState().logout();
  };
}
