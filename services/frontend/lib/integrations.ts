import { PLATFORM_DISPLAY_FIELD, type PlatformId } from './platforms';

export interface IntegrationLike {
  platform: string;
  externalId: string;
  metadata?: Record<string, unknown> | null;
}

export interface IntegrationDisplay {
  /** Primary human-readable name. Always non-empty. */
  name: string;
  /** Recognizable public handle; never a raw provider ID. */
  identifier: string;
  memberCount?: number;
}

// getIntegrationDisplay returns the canonical name + identifier pair to
// render an integration anywhere in the UI. The two-line layout (name on
// top, monospaced id below) is shared by the integrations card list and
// the disconnect confirmation dialog so the same row never appears with
// different labels in different places.
//
// Provider IDs remain internal. Public metadata supplies the name, handle
// and member count; absent metadata falls back to the platform label.
export function getIntegrationDisplay(
  integration: IntegrationLike,
  platformLabel: string
): IntegrationDisplay {
  const md = (integration.metadata ?? {}) as Record<string, unknown>;
  const id = integration.externalId;

  const field = PLATFORM_DISPLAY_FIELD[integration.platform as PlatformId];
  const friendly = field && typeof md[field] === 'string' ? (md[field] as string) : '';
  const display: IntegrationDisplay = { name: friendly || platformLabel, identifier: '' };
  if (integration.platform === 'telegram') {
    const username =
      typeof md.channel_username === 'string' ? md.channel_username : id.startsWith('@') ? id : '';
    if (/^@?[a-z\d_]{5,32}$/i.test(username)) display.identifier = `@${username.replace(/^@/, '')}`;
  }
  if (integration.platform === 'vk') {
    const raw = md.members_count;
    const count =
      typeof raw === 'number'
        ? raw
        : typeof raw === 'string' && /^\d+$/.test(raw)
          ? Number(raw)
          : NaN;
    if (Number.isInteger(count) && count >= 0) display.memberCount = count;
  }
  if (display.name === display.identifier) display.identifier = '';
  return display;
}
