// Frontend mirror of pkg/legalconfig/versions.go.
//
// Bumping any constant here triggers Surface E (ReConsentModal) for every
// authed user on a stale policy_version. MUST stay byte-mirrored with the
// Go constants in pkg/legalconfig/versions.go — ships a CI parity
// script (grep both files and diff). Mismatch → CI fails before deploy.
//
// v1.0 was the initial ship (effective_from 2026-06-01). v1.1 (effective_from
// 2026-09-08) moved the LLM processor to RF-hosted Yandex AI Studio, dropped
// the cross-border section, and added the third-party data / AI-content /
// processing-mandate clauses — see services/frontend/content/legal/{slug}.{locale}.md.

export const TOS_VERSION = 'v1.1';
export const PRIVACY_VERSION = 'v1.1';
export const PDN_VERSION = 'v1.1';

// PolicySlug — typed consent slug. Mirrors the user_consents.purpose column.
// Exactly these three values; marketing consent + 18+ confirmation deferred.
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
