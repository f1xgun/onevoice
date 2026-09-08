interface ConnectionState {
  businessId: string | null;
  pending: boolean;
  error: boolean;
  status?: string;
}

export function connectionStatus({ businessId, pending, error, status }: ConnectionState) {
  if (!businessId) return 'chooseOrganization';
  if (pending) return 'connectionLoading';
  if (error) return 'connectionUnknown';
  if (status === 'active') return 'connected';
  if (!status || status === 'inactive') return 'notConnected';
  return 'connectionAttention';
}
