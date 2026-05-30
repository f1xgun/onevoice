// Package legalconfig holds 152-ФЗ data-controller identity (legal entity)
// and the current policy versions. Imported by services/api
// (for /auth/consents validation + /auth/me requiresReconsent diff) and
// by future workers (RKN export, etc.). Mirrors services/frontend/lib/legal/.
//
package legalconfig

import "os"

// Entity describes the Russian PD operator (data controller) per
// 152-ФЗ Art. 14. All four fields come from the LEGAL_* env vars at
// API startup. When ANY field is empty or still placeholder, the
// renderer falls back to the «[Юридическое лицо — будет обновлено]»
// stub and the launch-readiness checklist flags the deployment.
type Entity struct {
	Name     string // LEGAL_ENTITY_NAME, default PlaceholderName.
	INN      string // LEGAL_INN, default "".
	Address  string // LEGAL_ADDRESS, default "".
	EmailPDN string // LEGAL_EMAIL_PDN, default PlaceholderEmail.
}

// PlaceholderName is the stub rendered when LEGAL_ENTITY_NAME is unset
// matches the frontend FE copy in 22-UI-SPEC.
const PlaceholderName = "[Юридическое лицо — будет обновлено]"

// PlaceholderEmail is the stub rendered when LEGAL_EMAIL_PDN is unset.
const PlaceholderEmail = "—"

// Load reads the four LEGAL_* env vars into an Entity. Missing values
// fall back to PlaceholderName / PlaceholderEmail (Name + Email) or to
// the empty string (INN + Address); IsPlaceholder inspects the result.
func Load() Entity {
	return Entity{
		Name:     getEnv("LEGAL_ENTITY_NAME", PlaceholderName),
		INN:      os.Getenv("LEGAL_INN"),
		Address:  os.Getenv("LEGAL_ADDRESS"),
		EmailPDN: getEnv("LEGAL_EMAIL_PDN", PlaceholderEmail),
	}
}

// IsPlaceholder reports whether ANY of the four fields is still in
// placeholder / empty state. The pre-launch checklist runs this
// against the loaded Entity at boot and fails staging deploys when it
// returns true.
func (e Entity) IsPlaceholder() bool {
	return e.Name == PlaceholderName ||
		e.Name == "" ||
		e.INN == "" ||
		e.Address == "" ||
		e.EmailPDN == PlaceholderEmail ||
		e.EmailPDN == ""
}

// getEnv reads the env var; falls back to def when empty.
func getEnv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
