import {
  channelConnectionState,
  type ConnectionIntegration,
} from '@/lib/constants/integrationStatus';

export interface ConnectionState {
  businessId: string | null;
  pending: boolean;
  error: boolean;
  integrations?: readonly ConnectionIntegration[];
}

export function connectionStatus({ businessId, pending, error, integrations }: ConnectionState) {
  if (!businessId) return 'chooseOrganization';
  if (pending) return 'connectionLoading';
  if (error) return 'connectionUnknown';
  const labels = {
    connected: 'connected',
    disconnected: 'notConnected',
    error: 'connectionAttention',
    unknown: 'connectionUnknown',
  } as const;
  return labels[channelConnectionState(integrations)];
}
