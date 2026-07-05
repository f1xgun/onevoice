'use client';

import { useQuery } from '@tanstack/react-query';

import { getBillingSummary, type BillingSummary } from '@/lib/api/billing';
import { extractApiErrorCode } from '@/lib/resolveErrorMap';

export interface UseBillingSummaryResult {
  data: BillingSummary | undefined;
  isLoading: boolean;
  isSuccess: boolean;
  error: (Error & { code?: string }) | null;
}

// Reads GET /businesses/{id}/billing/summary. Mirrors useAuditLogs: the fetch
// is wrapped so a backend error surfaces its code (`forbidden`, etc.) for the
// page's error mapping. Consumers gate loaded-only UI on `isSuccess` — a fetch
// error would otherwise leave `data` undefined while other flags read "settled".
export function useBillingSummary(businessID: string | null): UseBillingSummaryResult {
  const q = useQuery({
    queryKey: ['billing-summary', businessID] as const,
    queryFn: async () => {
      try {
        return await getBillingSummary(businessID as string);
      } catch (e) {
        const code = extractApiErrorCode(e) ?? 'unknown';
        const wrapped = new Error(code) as Error & { code?: string };
        wrapped.code = code;
        throw wrapped;
      }
    },
    enabled: !!businessID,
  });

  return {
    data: q.data,
    isLoading: q.isLoading,
    isSuccess: q.isSuccess,
    error: q.error as (Error & { code?: string }) | null,
  };
}
