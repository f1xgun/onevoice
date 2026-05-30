package legalconfig

// Current policy versions. Bumping these triggers the ReConsentModal for
// every user whose user_consents row has a stale policy_version.
// MUST mirror services/frontend/lib/legal/versions.ts (planner +
// executor add a CI check or a Makefile target that greps both).
//
// v1.0 is the initial ship (effective_from frontmatter date in
// services/frontend/content/legal/{slug}.{locale}.md).
const (
	TOSVersion     = "v1.0"
	PrivacyVersion = "v1.0"
	PDNVersion     = "v1.0"
)

// PolicySlug is the typed slug used by Register / ReConsent / Withdraw.
// Mirrors the user_consents.purpose column.
type PolicySlug string

// Slug constants — exactly these three values. Marketing consent
// and 18+ confirmation are deferred to v1.5.
const (
	PolicyTOS     PolicySlug = "tos"
	PolicyPrivacy PolicySlug = "privacy"
	PolicyPDN     PolicySlug = "pdn"
)

// CurrentVersion returns the build's current policy version for a given
// slug. Unknown slugs return "" so callers can detect a malformed
// payload (handler returns 400 consent_required).
func CurrentVersion(slug PolicySlug) string {
	switch slug {
	case PolicyTOS:
		return TOSVersion
	case PolicyPrivacy:
		return PrivacyVersion
	case PolicyPDN:
		return PDNVersion
	}
	return ""
}

// AllSlugs is the canonical iteration order over the three policy slugs.
// Used by handler/auth.go Register validation + service.ConsentService
// DiffAgainstCurrent to enumerate the user_consents rows that need to
// match currentVersion before a Register or ReConsent submit can land.
func AllSlugs() []PolicySlug {
	return []PolicySlug{PolicyTOS, PolicyPrivacy, PolicyPDN}
}
