// Integration row status vocabulary shared by PlatformCard. The localized
// status labels live under integrations.platformCard.status (RU+EN).
//
// The backend only ever writes `active` (set on a successful OAuth / paste-token
// connect; rows are soft-deleted on disconnect, so they vanish rather than
// flip to a "disconnected" status). The `inactive` / `error` / `pending_cookies`
// / `token_expired` badges had no writer and never rendered — they implied
// token-health tracking the API does not perform, so the card showed a stale
// "active" forever while claiming to surface other states. The vocabulary is
// narrowed to what is actually persisted so the badge stops lying. (Reconnect
// prompting on a dead token already works via the in-chat
// IntegrationTokenInvalidBanner, which fires on the agents'
// integration_token_invalid code.)
export type IntegrationStatus = 'active';

export const STATUS_LABEL_KEYS: readonly IntegrationStatus[] = ['active'] as const;

export const STATUS_TONES: Record<IntegrationStatus, 'success' | 'neutral' | 'danger' | 'warning'> =
  {
    active: 'success',
  };
