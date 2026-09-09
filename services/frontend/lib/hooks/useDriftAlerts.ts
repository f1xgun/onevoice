'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  fetchDriftAlerts,
  updateDriftAlerts,
  type DriftAlertSettings,
} from '@/lib/api/driftAlerts';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';

export function useDriftAlerts(businessId: string, enabled: boolean) {
  return useQuery({
    queryKey: QUERY_KEYS.BUSINESS_DRIFT_ALERTS(businessId),
    queryFn: () => fetchDriftAlerts(businessId),
    enabled,
  });
}

export function useUpdateDriftAlerts() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ businessId, settings }: { businessId: string; settings: DriftAlertSettings }) =>
      updateDriftAlerts(businessId, settings),
    onSuccess: (settings, variables) =>
      queryClient.setQueryData(QUERY_KEYS.BUSINESS_DRIFT_ALERTS(variables.businessId), settings),
  });
}
