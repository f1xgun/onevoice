// Maps a consent-purpose slug to its public /legal/* page route.
//
// PolicySlug (tos/privacy/pdn) mirrors the user_consents.purpose column and is
// the backend-shared consent key. The public legal pages, however, live at
// human-readable routes (/legal/terms, /legal/privacy, /legal/consent) and the
// content files are named after those routes (terms.*.md, consent.*.md). So the
// two differ: tos→terms, pdn→consent. Building `/legal/${policySlug}` directly
// 404s for tos and pdn — use legalDocHref instead.

import type { PolicySlug } from '@/lib/legal/versions';

export const POLICY_SLUG_TO_ROUTE: Record<PolicySlug, string> = {
  tos: 'terms',
  privacy: 'privacy',
  pdn: 'consent',
};

export function legalDocHref(slug: PolicySlug): string {
  return `/legal/${POLICY_SLUG_TO_ROUTE[slug]}`;
}
