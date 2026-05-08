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

func TestPlatforms_NoEmptyMetadata(t *testing.T) {
	for _, p := range Platforms() {
		if p.Name == "" {
			t.Errorf("%s: empty Name", p.ID)
		}
		if p.Description == "" {
			t.Errorf("%s: empty Description", p.ID)
		}
	}
}
