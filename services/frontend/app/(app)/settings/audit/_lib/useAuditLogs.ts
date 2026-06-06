'use client';

import { useInfiniteQuery } from '@tanstack/react-query';
import { api } from '@/lib/api';
import type { AuditFilters, AuditLogDTO, AuditLogListResponse } from './types';

// Default page size — matches the backend default (handler/audit_log.go).
// Max allowed by backend is 200; we stick to 50 to keep the table responsive.
const PAGE_SIZE = 50;

async function fetchPage(
  businessID: string,
  filters: AuditFilters,
  cursor?: string
): Promise<AuditLogListResponse> {
  const params = new URLSearchParams();
  params.set('limit', String(PAGE_SIZE));
  if (cursor) params.set('cursor', cursor);
  if (filters.category && filters.category !== 'all') params.set('category', filters.category);
  if (filters.action) params.set('action', filters.action);
  if (filters.actorID) params.set('actor', filters.actorID);
  if (filters.from) params.set('from', filters.from);
  if (filters.to) params.set('to', filters.to);

  try {
    const { data } = await api.get<AuditLogListResponse>(
      `/businesses/${businessID}/audit-logs?${params.toString()}`
    );
    return data;
  } catch (e) {
    const err = e as { response?: { data?: { error?: string } } };
    const code = err?.response?.data?.error ?? 'unknown';
    const wrapped = new Error(code);
    (wrapped as Error & { code?: string }).code = code;
    throw wrapped;
  }
}

export function useAuditLogs(businessID: string | null, filters: AuditFilters) {
  const q = useInfiniteQuery({
    queryKey: ['audit-logs', businessID, filters] as const,
    queryFn: ({ pageParam }) =>
      fetchPage(businessID as string, filters, pageParam as string | undefined),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor ?? undefined,
    enabled: !!businessID,
  });
  const items: AuditLogDTO[] = q.data?.pages.flatMap((p) => p.items) ?? [];
  return {
    items,
    hasNextPage: q.hasNextPage,
    fetchNextPage: q.fetchNextPage,
    isLoading: q.isLoading,
    isFetching: q.isFetching,
    isFetchingNextPage: q.isFetchingNextPage,
    error: q.error as (Error & { code?: string }) | null,
  };
}
