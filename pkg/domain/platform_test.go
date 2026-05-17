package domain

import "testing"

func TestPlatforms_DisplayOrder(t *testing.T) {
	got := Platforms()
	wantOrder := []string{
		"telegram",
		"vk",
		"yandex_business",
		"google_business",
		"2gis",
		"avito",
		"whatsapp",
	}
	if len(got) != len(wantOrder) {
		t.Fatalf("got %d platforms, want %d", len(got), len(wantOrder))
	}
	for i, p := range got {
		if p.ID != wantOrder[i] {
			t.Errorf("position %d: got %q, want %q", i, p.ID, wantOrder[i])
		}
	}
}

func TestPlatforms_DefaultStatuses(t *testing.T) {
	want := map[string]PlatformStatus{
		"telegram":        PlatformStatusActive,
		"vk":              PlatformStatusActive,
		"yandex_business": PlatformStatusActive,
		// google_business is held in coming_soon despite the agent existing,
		// per Linen design v2 product decision.
		"google_business": PlatformStatusComingSoon,
		"2gis":            PlatformStatusComingSoon,
		"avito":           PlatformStatusComingSoon,
		"whatsapp":        PlatformStatusComingSoon,
	}
	for _, p := range Platforms() {
		if p.Status != want[p.ID] {
			t.Errorf("%s: got status %q, want %q", p.ID, p.Status, want[p.ID])
		}
	}
}

// TestPlatforms_NameDescriptionDropped pins the i18n Phase C2 invariant:
// Name and Description are no longer populated by the registry. The
// frontend renders both via its messages/*.json bundles. Re-introducing
// values here would silently revert clients that have already migrated
// to the i18n flow.
func TestPlatforms_NameDescriptionDropped(t *testing.T) {
	for _, p := range Platforms() {
		if p.Name != "" {
			t.Errorf("%s: expected empty Name (Phase C2), got %q", p.ID, p.Name)
		}
		if p.Description != "" {
			t.Errorf("%s: expected empty Description (Phase C2), got %q", p.ID, p.Description)
		}
	}
}
