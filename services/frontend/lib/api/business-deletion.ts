// frontend client for organization (business) deletion endpoints.
//
// DELETE /api/v1/businesses/{id}          → 204 / 403 / 404 / 423
// POST   /api/v1/businesses/{id}/restore  → 204 / 403 / 404 / 410

import { useAuthStore } from '@/lib/auth';

const HTTP_NO_CONTENT = 204;

export interface BusinessDeletionError {
  code?: string;
  status?: number;
  // 423 business_pending_deletion body
  deletionDate?: string;
  restoreUrl?: string;
  message?: string;
}

/**
 * deleteBusiness sends DELETE /api/v1/businesses/{id}. Resolves void on 204.
 * Throws a BusinessDeletionError with `code` / `status` / payload on failure
 * so the caller can branch by code (mirrors lib/api/account.ts).
 */
export async function deleteBusiness(businessId: string): Promise<void> {
  const token = useAuthStore.getState().accessToken;
  const res = await fetch(`/api/v1/businesses/${businessId}`, {
    method: 'DELETE',
    headers: {
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    credentials: 'include',
  });

  if (res.status === HTTP_NO_CONTENT) {
    return;
  }

  let body: BusinessDeletionError = {};
  try {
    body = (await res.json()) as BusinessDeletionError;
  } catch {}
  body.status = res.status;
  throw body;
}

/**
 * restoreBusiness sends POST /api/v1/businesses/{id}/restore. The Origin
 * header is set automatically by the browser; the backend's CSRF guard checks
 * it against the allowed-origins list. Throws on non-204 with the same shape
 * as deleteBusiness.
 */
export async function restoreBusiness(businessId: string): Promise<void> {
  const token = useAuthStore.getState().accessToken;
  const res = await fetch(`/api/v1/businesses/${businessId}/restore`, {
    method: 'POST',
    headers: {
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    credentials: 'include',
  });

  if (res.status === HTTP_NO_CONTENT) {
    return;
  }
  let body: BusinessDeletionError = {};
  try {
    body = (await res.json()) as BusinessDeletionError;
  } catch {}
  body.status = res.status;
  throw body;
}
