// Integration row status vocabulary shared by PlatformCard. The localized
// status labels live under integrations.platformCard.status (RU+EN).
//
// Two statuses are actually persisted today:
//   - `active`        — set on a successful OAuth / paste-token connect.
//   - `token_expired` — written event-driven when a platform agent rejects the
//     token/session (typed code `integration_token_invalid`); the backend
//     flips the stored status so the card surfaces a Reconnect affordance.
//
// The other badges that once lived here (`inactive` / `error` /
// `pending_cookies`) are intentionally omitted — no backend writer ever sets
// them, so they implied state tracking the API does not perform. Disconnect
// soft-deletes the row, so it vanishes rather than flipping to a status.
export type IntegrationStatus = 'active' | 'token_expired';

export const STATUS_LABEL_KEYS: readonly IntegrationStatus[] = ['active', 'token_expired'] as const;

export const STATUS_TONES: Record<IntegrationStatus, 'success' | 'neutral' | 'danger' | 'warning'> =
  {
    active: 'success',
    token_expired: 'danger',
  };
