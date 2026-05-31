// Integration row status vocabulary shared by PlatformCard. The
// localized status labels live under integrations.platformCard.status
// (RU+EN); STATUS_LABEL_KEYS is the read-only tuple a card uses to
// gate `tCard('status.<id>')` lookups, and STATUS_TONES maps the same
// vocabulary to the <Badge tone=…> prop literal union.

export type IntegrationStatus =
  | 'active'
  | 'inactive'
  | 'error'
  | 'pending_cookies'
  | 'token_expired';

export const STATUS_LABEL_KEYS: readonly IntegrationStatus[] = [
  'active',
  'inactive',
  'error',
  'pending_cookies',
  'token_expired',
] as const;

export const STATUS_TONES: Record<IntegrationStatus, 'success' | 'neutral' | 'danger' | 'warning'> =
  {
    active: 'success',
    inactive: 'neutral',
    error: 'danger',
    pending_cookies: 'warning',
    token_expired: 'danger',
  };
