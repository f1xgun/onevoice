'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import {
  fetchIntegrationsDrift,
  verifyIntegrations,
  type IntegrationDrift,
  type VerifyIntegrationsResponse,
} from '@/lib/api/integrationsSync';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';

// useIntegrationsDrift reads GET /businesses/{id}/integrations/drift. Consumers
// gate loaded-only UI on `isSuccess` — a fetch error would otherwise leave
// `data` undefined while other flags read "settled".
export function useIntegrationsDrift(businessId: string | null) {
  return useQuery<IntegrationDrift[]>({
    queryKey: QUERY_KEYS.BUSINESS_INTEGRATIONS_DRIFT(businessId),
    queryFn: () => fetchIntegrationsDrift(businessId as string),
    enabled: !!businessId,
  });
}

// useVerifyIntegrations POSTs the manual repair (re-push + reschedule) and
// invalidates the drift query so the refreshed status is refetched. Toast copy
// is left to the caller so it stays in the view layer.
export function useVerifyIntegrations(businessId: string | null) {
  const qc = useQueryClient();
  return useMutation<VerifyIntegrationsResponse, Error, void>({
    mutationFn: () => {
      if (!businessId) return Promise.reject(new Error('No active business'));
      return verifyIntegrations(businessId);
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: QUERY_KEYS.BUSINESS_INTEGRATIONS_DRIFT(businessId) });
    },
  });
}
