// Frontend client for the backend consent
// endpoints (mirrors the shape of lib/api/account.ts).
//
// Endpoints (contract):
// POST /api/v1/auth/consents                        → 204 / 400 / 409 / 403
// POST /api/v1/users/me/consents/pdn/withdraw       → 204 / 423 / 403 / 404
// GET  /api/v1/users/me/consents                    → { consents: ConsentRecord[] }
//
// None of these endpoints emits a business-logic 401 — a 401 here only ever
// comes from the auth middleware (the access token expired), so routing them
// through authFetch (which refreshes on 401) is safe.
//
// All POSTs include credentials:'include' so the backend's Origin CSRF
// guard (mirrors user_deletion.go shape per 22-01 SUMMARY line 151)
// accepts the request. authFetch attaches the in-memory access token and
// transparently refreshes it on a 401 (shared single-flight).

import { authFetch } from '@/lib/api/authFetch';
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

/**
 * postReconsent sends POST /api/v1/auth/consents with the per-policy
 * version+sha256 array. Returns void on 204 success. Throws a
 * ConsentError with `code`/`status` on failure so the caller can branch
 * (409 version_mismatch → toast + reload).
 */
export async function postReconsent(policies: ConsentPolicy[]): Promise<void> {
  const res = await authFetch('/api/v1/auth/consents', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
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
  const res = await authFetch('/api/v1/users/me/consents/pdn/withdraw', {
    method: 'POST',
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
  const res = await authFetch('/api/v1/users/me/consents', {
    credentials: 'include',
  });
  if (!res.ok) {
    throw { status: res.status } as ConsentError;
  }
  const data = (await res.json()) as { consents: ConsentRecord[] };
  return data.consents;
}
