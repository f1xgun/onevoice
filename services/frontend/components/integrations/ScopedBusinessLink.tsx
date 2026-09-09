'use client';

import { useEffect } from 'react';
import { useSearchParams } from 'next/navigation';

import { useBusinessList } from '@/lib/hooks/useBusinessList';
import { useBusinessStore } from '@/lib/stores/business';

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

export function resolveScopedBusinessId(
  requestedId: string | null,
  memberships: ReadonlyArray<{ id: string; deletion_pending_until?: string }>
): string | null {
  if (!requestedId || !UUID_RE.test(requestedId)) return null;
  return memberships.some(
    (business) => business.id === requestedId && !business.deletion_pending_until
  )
    ? requestedId
    : null;
}

// Consumes the organization UUID emitted by drift-alert links. The membership
// list is authoritative, so a malformed, deleted, or inaccessible UUID cannot
// change the active organization.
export function ScopedBusinessLink() {
  const searchParams = useSearchParams();
  const requestedId = searchParams.get('businessId');
  const { data: memberships } = useBusinessList();
  const setActive = useBusinessStore((state) => state.setActive);

  useEffect(() => {
    if (!requestedId || !memberships) return;
    const businessId = resolveScopedBusinessId(requestedId, memberships);
    if (businessId) setActive(businessId);

    const url = new URL(window.location.href);
    url.searchParams.delete('businessId');
    window.history.replaceState({}, '', url.pathname + url.search + url.hash);
  }, [memberships, requestedId, setActive]);

  return null;
}
