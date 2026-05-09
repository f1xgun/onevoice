'use client';

import { useQuery } from '@tanstack/react-query';
import { api } from '@/lib/api';

export interface BusinessSummary {
  id: string;
  name: string;
  role: { id: string; name: string };
  status: 'active' | 'suspended';
  joined_at: string;
}

export const BUSINESS_LIST_QUERY_KEY = ['businesses'] as const;

export function useBusinessList() {
  return useQuery<BusinessSummary[]>({
    queryKey: BUSINESS_LIST_QUERY_KEY,
    queryFn: async () => {
      const { data } = await api.get<BusinessSummary[]>('/businesses');
      return data;
    },
  });
}
