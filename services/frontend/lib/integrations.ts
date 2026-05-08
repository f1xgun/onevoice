export interface IntegrationLike {
  platform: string;
  externalId: string;
  metadata?: Record<string, unknown> | null;
}

export interface IntegrationDisplay {
  /** Primary human-readable name. Always non-empty. */
  name: string;
  /** Mono identifier shown beneath the name. Empty when name *is* the id. */
  identifier: string;
}

// getIntegrationDisplay returns the canonical name + identifier pair to
// render an integration anywhere in the UI. The two-line layout (name on
// top, monospaced id below) is shared by the integrations card list and
// the disconnect confirmation dialog so the same row never appears with
// different labels in different places.
//
// Per-platform sources:
//   - telegram: metadata.channel_title (from getChat) above @username/-100…
//   - vk:       metadata.community_name (from groups.getById, lazily backfilled)
//               above the numeric group_id
//   - yandex_business: metadata.yandex_user (Passport login captured at
//               connect time) above the externalId; for the legacy
//               externalId="default" placeholder, we fall back to the
//               platform label so we never render the literal "default".
//
// Fallback order is name → identifier so the user always sees *something*
// resolvable; identifier is suppressed when it would just duplicate the name.
export function getIntegrationDisplay(
  integration: IntegrationLike,
  platformLabel: string
): IntegrationDisplay {
  const md = (integration.metadata ?? {}) as Record<string, unknown>;
  const id = integration.externalId;

  const friendly = (() => {
    switch (integration.platform) {
      case 'telegram':
        return typeof md.channel_title === 'string' ? md.channel_title : '';
      case 'vk':
        return typeof md.community_name === 'string' ? md.community_name : '';
      case 'yandex_business':
        // business_name is the Sprav profile name (e.g. "Кафе Ромашка"),
        // resolved lazily via the agent's get_info RPA tool — see
        // POST /integrations/yandex_business/{id}/refresh-name.
        return typeof md.business_name === 'string' ? md.business_name : '';
      default:
        return '';
    }
  })();

  // Yandex's externalId="default" is a placeholder, not a real identifier —
  // never render it. Show the platform label as the name when no friendly
  // value is available, with no identifier line at all.
  if (integration.platform === 'yandex_business' && id === 'default') {
    return { name: friendly || platformLabel, identifier: friendly ? '' : '' };
  }

  if (friendly) {
    // Don't echo the same string on both lines.
    return { name: friendly, identifier: friendly === id ? '' : id };
  }
  return { name: id || platformLabel, identifier: '' };
}
