'use client';

import { useEffect, useRef } from 'react';

import {
  trackApprovalEvent,
  type ApprovalKind,
  type ApprovalSurface,
} from '@/lib/approvalTelemetry';

/** Count a rendered draft once per mount, including React StrictMode effect replay. */
export function useApprovalImpressions(
  identity: string,
  kind: ApprovalKind | undefined,
  source: ApprovalSurface,
  visible = true,
  approvalVisible = visible
): void {
  const seen = useRef(new Set<string>());
  useEffect(() => {
    if (!kind) return;
    for (const [action, shown] of [
      ['draft_shown', visible],
      ['approval_shown', approvalVisible],
    ] as const) {
      const key = `${identity}:${action}`;
      if (!shown || seen.current.has(key)) continue;
      seen.current.add(key);
      void trackApprovalEvent(action, identity, kind, source);
    }
  }, [identity, kind, source, visible, approvalVisible]);
}
