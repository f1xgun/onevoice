package legalconfig

// Current policy versions. Bumping these triggers the ReConsentModal for
// every user whose user_consents row has a stale policy_version.
// MUST mirror services/frontend/lib/legal/versions.ts.
const (
	TOSVersion     = "v1.1"
	PrivacyVersion = "v1.1"
	PDNVersion     = "v1.1"
)

// PolicySlug is the typed slug for Register / ReConsent / Withdraw.
// Mirrors the user_consents.purpose column.
type PolicySlug string

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
func AllSlugs() []PolicySlug {
	return []PolicySlug{PolicyTOS, PolicyPrivacy, PolicyPDN}
}
