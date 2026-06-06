// Frontend client for the backend consent
// endpoints (mirrors the shape of lib/api/account.ts).
//
// Endpoints (contract):
// POST /api/v1/auth/consents                        → 204 / 409 / 401 / 423
// POST /api/v1/users/me/consents/pdn/withdraw       → 204 / 423
// GET  /api/v1/users/me/consents                    → { consents: ConsentRecord[] }
//
// All POSTs include credentials:'include' so the backend's Origin CSRF
// guard (mirrors user_deletion.go shape per 22-01 SUMMARY line 151)
// accepts the request. Authorization header comes from the in-memory
// access token (refreshed by the silent-refresh flow on layout mount).

import { useAuthStore } from '@/lib/auth';
import type { PolicySlug } from '@/lib/legal/versions';

const HTTP_NO_CONTENT = 204;

export interface ConsentPolicy {
  slug: PolicySlug;
  version: string;
  sha256?: string;
}

export interface ConsentRecord {
  slug: PolicySlug;
  version: string;
  // Both `sha256` and `withdrawnAt` are spec-side optional (omitempty).
  // Backend OMITS the field when the value is absent (empty string for the
  // hash, nil time for active rows) — frontend treats missing == not-set.
  sha256?: string;
  acceptedAt: string;
  withdrawnAt?: string;
}

export interface ConsentError {
  code?: string;
  status?: number;
  missing?: string[];
  currentVersion?: string;
  deletionDate?: string;
  restoreUrl?: string;
  message?: string;
}

function authHeaders(extra: HeadersInit = {}): HeadersInit {
  const token = useAuthStore.getState().accessToken;
  return {
    ...extra,
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
  };
}

/**
 * postReconsent sends POST /api/v1/auth/consents with the per-policy
 * version+sha256 array. Returns void on 204 success. Throws a
 * ConsentError with `code`/`status` on failure so the caller can branch
 * (409 version_mismatch → toast + reload).
 */
export async function postReconsent(policies: ConsentPolicy[]): Promise<void> {
  const res = await fetch('/api/v1/auth/consents', {
    method: 'POST',
    headers: authHeaders({ 'Content-Type': 'application/json' }),
    credentials: 'include',
    body: JSON.stringify({ policies }),
  });

  if (res.status === HTTP_NO_CONTENT) {
    return;
  }
  let body: ConsentError = {};
  try {
    body = (await res.json()) as ConsentError;
  } catch {}
  body.status = res.status;
  throw body;
}

/**
 * withdrawPDN sends POST /api/v1/users/me/consents/pdn/withdraw with no
 * body. Backend triggers AccountDeletionService.RequestDeletion(reason=
 * 'consent_withdrawn') from (same 30-day grace + restore
 * window). Returns void on 204; throws on non-204 with the same shape.
 * 423 account_pending_deletion is treated as success by the caller per
 * UI-SPEC §F edge case.
 */
export async function withdrawPDN(): Promise<void> {
  const res = await fetch('/api/v1/users/me/consents/pdn/withdraw', {
    method: 'POST',
    headers: authHeaders(),
    credentials: 'include',
  });

  if (res.status === HTTP_NO_CONTENT) {
    return;
  }
  let body: ConsentError = {};
  try {
    body = (await res.json()) as ConsentError;
  } catch {}
  body.status = res.status;
  throw body;
}

/**
 * listMyConsents fetches GET /api/v1/users/me/consents — returns the
 * three current consent rows (tos, privacy, pdn) for the authed user.
 * Withdrawn rows still appear with `withdrawnAt` set. Consumed by
 * Surface F (WithdrawalPanel).
 */
export async function listMyConsents(): Promise<ConsentRecord[]> {
  const res = await fetch('/api/v1/users/me/consents', {
    headers: authHeaders(),
    credentials: 'include',
  });
  if (!res.ok) {
    throw { status: res.status } as ConsentError;
  }
  const data = (await res.json()) as { consents: ConsentRecord[] };
  return data.consents;
}
