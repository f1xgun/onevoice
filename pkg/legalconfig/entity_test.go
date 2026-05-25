package legalconfig

import (
	"testing"
)

// TestEntity_Load_DefaultsWhenEnvUnset verifies that Load() returns
// the placeholder defaults specified in D-19/D-22 when no LEGAL_* env
// vars are set. t.Setenv guarantees the cleanup pops back to ambient
// state for sibling tests.
func TestEntity_Load_DefaultsWhenEnvUnset(t *testing.T) {
	t.Setenv("LEGAL_ENTITY_NAME", "")
	t.Setenv("LEGAL_INN", "")
	t.Setenv("LEGAL_ADDRESS", "")
	t.Setenv("LEGAL_EMAIL_PDN", "")

	e := Load()
	if e.Name != PlaceholderName {
		t.Errorf("Name: got %q, want %q", e.Name, PlaceholderName)
	}
	if e.EmailPDN != PlaceholderEmail {
		t.Errorf("EmailPDN: got %q, want %q", e.EmailPDN, PlaceholderEmail)
	}
	if e.INN != "" {
		t.Errorf("INN: got %q, want empty", e.INN)
	}
	if e.Address != "" {
		t.Errorf("Address: got %q, want empty", e.Address)
	}
}

// TestEntity_Load_ReadsAllFour verifies that Load() reflects every
// LEGAL_* env var when populated. Mirrors the production-deploy story
// where the operator fills in the 4 values from docs/runbook-rkn-filing.md.
func TestEntity_Load_ReadsAllFour(t *testing.T) {
	t.Setenv("LEGAL_ENTITY_NAME", "ООО ВанВойс")
	t.Setenv("LEGAL_INN", "7723456789")
	t.Setenv("LEGAL_ADDRESS", "Москва, ул. Пример, 1")
	t.Setenv("LEGAL_EMAIL_PDN", "pdn@onevoice.app")

	e := Load()
	if e.Name != "ООО ВанВойс" {
		t.Errorf("Name: got %q", e.Name)
	}
	if e.INN != "7723456789" {
		t.Errorf("INN: got %q", e.INN)
	}
	if e.Address != "Москва, ул. Пример, 1" {
		t.Errorf("Address: got %q", e.Address)
	}
	if e.EmailPDN != "pdn@onevoice.app" {
		t.Errorf("EmailPDN: got %q", e.EmailPDN)
	}
}

// TestEntity_IsPlaceholder_TrueWhenAnyFieldIsPlaceholderOrEmpty asserts
// the 22-CONTEXT D-22 invariant: if any of the 4 fields is still a
// placeholder or blank, IsPlaceholder() reports true so the frontend +
// pre-launch checklist (D-29) can flag the deployment as not yet
// production-ready.
func TestEntity_IsPlaceholder_TrueWhenAnyFieldIsPlaceholderOrEmpty(t *testing.T) {
	cases := []struct {
		name string
		e    Entity
		want bool
	}{
		{
			name: "all placeholder",
			e:    Entity{Name: PlaceholderName, INN: "", Address: "", EmailPDN: PlaceholderEmail},
			want: true,
		},
		{
			name: "all populated",
			e:    Entity{Name: "ООО ВанВойс", INN: "7723456789", Address: "Москва, ...", EmailPDN: "pdn@x.ru"},
			want: false,
		},
		{
			name: "empty address",
			e:    Entity{Name: "ООО ВанВойс", INN: "7723456789", Address: "", EmailPDN: "pdn@x.ru"},
			want: true,
		},
		{
			name: "placeholder email",
			e:    Entity{Name: "ООО ВанВойс", INN: "7723456789", Address: "Москва, ...", EmailPDN: PlaceholderEmail},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.e.IsPlaceholder(); got != tc.want {
				t.Errorf("IsPlaceholder() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestCurrentVersion_ReturnsConstantPerSlug verifies the build's policy
// version map. Bumping any of these constants in versions.go triggers
// ReConsentModal for every user whose user_consents.policy_version is
// stale (D-10).
func TestCurrentVersion_ReturnsConstantPerSlug(t *testing.T) {
	cases := map[PolicySlug]string{
		PolicyTOS:     TOSVersion,
		PolicyPrivacy: PrivacyVersion,
		PolicyPDN:     PDNVersion,
	}
	for slug, want := range cases {
		if got := CurrentVersion(slug); got != want {
			t.Errorf("CurrentVersion(%q) = %q, want %q", slug, got, want)
		}
	}
	if got := CurrentVersion("unknown"); got != "" {
		t.Errorf("CurrentVersion(unknown) = %q, want empty", got)
	}
}
