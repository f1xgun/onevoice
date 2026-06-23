// frontend client for account-deletion endpoints.
//
// Endpoints:
// DELETE /api/v1/users/me          {password}  → 204 / 401 / 409 / 423
// POST   /api/v1/users/me/restore  (no body)   → 204 / 403 / 404 / 410

import { useAuthStore } from '@/lib/auth';
import { authFetch } from '@/lib/api/authFetch';

const HTTP_OK = 200;
const HTTP_NO_CONTENT = 204;

export interface SoleOwnerBusiness {
  id: string;
  name: string;
}

export interface DeletionAccountError {
  code?: string;
  status?: number;
  // 409 sole_owner_of_businesses body
  businesses?: SoleOwnerBusiness[];
  // 423 account_pending_deletion body
  deletionDate?: string;
  restoreUrl?: string;
  message?: string;
}

/**
 * deleteAccount sends DELETE /api/v1/users/me with the password. Returns
 * void on 204 success. Throws a DeletionAccountError with `code` /
 * `status` / payload on failure so the caller can branch by code.
 *
 * Stays on raw fetch (not authFetch): this endpoint returns 401
 * `password_invalid` for a wrong password, so a 401 here is NOT a
 * session-expiry signal — routing it through authFetch would fire a
 * needless token refresh on every wrong password (and could log the user
 * out on a flaky refresh). The access token is fresh anyway: the user is
 * actively typing into the confirm-password modal.
 */
export async function deleteAccount(password: string): Promise<void> {
  const token = useAuthStore.getState().accessToken;
  const res = await fetch('/api/v1/users/me', {
    method: 'DELETE',
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    credentials: 'include',
    body: JSON.stringify({ password }),
  });

  if (res.status === HTTP_NO_CONTENT) {
    return;
  }

  let body: DeletionAccountError = {};
  try {
    body = (await res.json()) as DeletionAccountError;
  } catch {}
  body.status = res.status;
  throw body;
}

/**
 * restoreAccount sends POST /api/v1/users/me/restore. The Origin header
 * is set automatically by the browser; the backend's CSRF guard
 * (T-DEL-10) checks it against the allowed-origins list. Throws on
 * non-204 with the same shape as deleteAccount.
 */
export async function restoreAccount(): Promise<void> {
  const res = await authFetch('/api/v1/users/me/restore', {
    method: 'POST',
    credentials: 'include',
  });

  if (res.status === HTTP_NO_CONTENT) {
    return;
  }
  let body: DeletionAccountError = {};
  try {
    body = (await res.json()) as DeletionAccountError;
  } catch {}
  body.status = res.status;
  throw body;
}

// Re-export the success status constants so callers can compare without
// re-hard-coding magic numbers in their own modules.
export const HTTP_STATUS_OK = HTTP_OK;
export const HTTP_STATUS_NO_CONTENT = HTTP_NO_CONTENT;
