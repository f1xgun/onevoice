'use client';

import { useQuery } from '@tanstack/react-query';
import { bizApi } from '@/lib/api/business-api';
import { useBusinessStore } from '@/lib/stores/business';
import type { Review } from '@/types/review';

export interface DecisionItem {
  id: string;
  kind: 'review' | 'approval_batch' | 'connection' | 'drift';
  platform?: string;
  createdAt: string;
  review?: Review;
  conversationId?: string;
  callCount?: number;
  drift?: { driftFields: string[] };
}
export interface DecisionQueue {
  items: DecisionItem[];
  total: number;
  automatic: { id: string; action: string; createdAt: string }[];
}
export function decisionQueueKey(businessId: string | null) {
  return ['businesses', businessId, 'decision-queue'] as const;
}
export function useDecisionQueue() {
  const businessId = useBusinessStore((s) => s.activeBusinessId);
  return useQuery({
    queryKey: decisionQueueKey(businessId),
    queryFn: async () => (await bizApi(businessId!).get<DecisionQueue>('/decision-queue')).data,
    enabled: !!businessId,
    refetchInterval: 30000,
  });
}
