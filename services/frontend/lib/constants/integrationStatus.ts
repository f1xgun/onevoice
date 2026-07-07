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

// Connection-health verdict written into integration.metadata.connection_health
// by the API (per-integration liveness, distinct from the composite presence
// score). Only `broken` and `degraded` surface a badge; `active` reuses the
// normal status row, and `unknown`/absent renders nothing so an inconclusive
// probe never alarms the user.
export type ConnectionHealthStatus = 'active' | 'degraded' | 'broken' | 'unknown';

// ConnectionHealth mirrors the metadata.connection_health sub-object. reason_code
// is a stable machine string resolved to copy via
// integrations.platformCard.health.reason.<code>.
export interface ConnectionHealth {
  status?: ConnectionHealthStatus;
  reason_code?: string;
  checked_at?: string;
}

// CONNECTION_HEALTH_TONES maps the two alarming verdicts to a Badge tone; the
// FE renders nothing for active/unknown/absent.
export const CONNECTION_HEALTH_TONES: Record<'broken' | 'degraded', 'danger' | 'warning'> = {
  broken: 'danger',
  degraded: 'warning',
};

// readConnectionHealth safely extracts the connection_health sub-object from an
// integration's metadata, returning undefined when absent or malformed.
export function readConnectionHealth(
  metadata: Record<string, unknown> | undefined
): ConnectionHealth | undefined {
  if (!metadata) return undefined;
  const raw = metadata['connection_health'];
  if (!raw || typeof raw !== 'object') return undefined;
  return raw as ConnectionHealth;
}
