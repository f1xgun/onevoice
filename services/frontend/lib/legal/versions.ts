// Frontend mirror of pkg/legalconfig/versions.go.
//
// Bumping any constant here triggers Surface E (ReConsentModal) for every
// authed user on a stale policy_version. MUST stay byte-mirrored with the
// Go constants in pkg/legalconfig/versions.go — ships a CI parity
// script (grep both files and diff). Mismatch → CI fails before deploy.
//
// v1.0 is the initial ship (effective_from frontmatter date
// 2026-06-01 in services/frontend/content/legal/{slug}.{locale}.md).

export const TOS_VERSION = 'v1.0';
export const PRIVACY_VERSION = 'v1.0';
export const PDN_VERSION = 'v1.0';

// PolicySlug is the typed slug used by Register / ReConsent / Withdraw.
// Mirrors the user_consents.purpose column. Exactly these three
// values; marketing consent + 18+ confirmation deferred to v1.5 per
// 22-/.
export type PolicySlug = 'tos' | 'privacy' | 'pdn';

// currentVersion returns the build's current policy version for a slug.
// Mirrors pkg/legalconfig.CurrentVersion — server cross-checks every
// (slug,version) on POST /auth/consents and POST /auth/register, so a
// drift between these two files is detected at request time as 400
// consent_required.
export function currentVersion(slug: PolicySlug): string {
  switch (slug) {
    case 'tos':
      return TOS_VERSION;
    case 'privacy':
      return PRIVACY_VERSION;
    case 'pdn':
      return PDN_VERSION;
  }
}

// ALL_SLUGS is the canonical iteration order. Mirrors pkg/legalconfig.AllSlugs.
export const ALL_SLUGS: readonly PolicySlug[] = ['tos', 'privacy', 'pdn'] as const;
