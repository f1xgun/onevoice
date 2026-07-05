package platform

import "testing"

// TestOwnerBriefFromSettings_DefaultOn asserts the brief is enabled when the key
// is absent or when enabled is unset — the default-on guarantee that makes the
// feature live for every business without a migration/backfill.
func TestOwnerBriefFromSettings_DefaultOn(t *testing.T) {
	cases := []struct {
		name     string
		settings map[string]interface{}
	}{
		{"nil settings", nil},
		{"no ownerBrief key", map[string]interface{}{"voiceProfile": "x"}},
		{"ownerBrief present without enabled", map[string]interface{}{
			"ownerBrief": map[string]interface{}{"weekday": float64(2)},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := OwnerBriefFromSettings(tc.settings)
			if !got.Enabled {
				t.Errorf("Enabled = false, want true (default-on) for %s", tc.name)
			}
		})
	}
}

// TestOwnerBriefFromSettings_ExplicitOptOut asserts enabled=false only when the
// stored object explicitly sets it false.
func TestOwnerBriefFromSettings_ExplicitOptOut(t *testing.T) {
	got := OwnerBriefFromSettings(map[string]interface{}{
		"ownerBrief": map[string]interface{}{"enabled": false},
	})
	if got.Enabled {
		t.Error("Enabled = true, want false after explicit opt-out")
	}
}

// TestOwnerBriefFromSettings_WeekdayHour asserts weekday/hour are read when valid
// and fall back to the Monday-09:00 defaults when out of range.
func TestOwnerBriefFromSettings_WeekdayHour(t *testing.T) {
	got := OwnerBriefFromSettings(map[string]interface{}{
		"ownerBrief": map[string]interface{}{"enabled": true, "weekday": float64(3), "hour": float64(14)},
	})
	if got.Weekday != 3 || got.Hour != 14 {
		t.Errorf("weekday/hour = %d/%d, want 3/14", got.Weekday, got.Hour)
	}

	outOfRange := OwnerBriefFromSettings(map[string]interface{}{
		"ownerBrief": map[string]interface{}{"weekday": float64(9), "hour": float64(30)},
	})
	if outOfRange.Weekday != DefaultOwnerBriefWeekday || outOfRange.Hour != DefaultOwnerBriefHour {
		t.Errorf("out-of-range weekday/hour must fall back to defaults, got %d/%d", outOfRange.Weekday, outOfRange.Hour)
	}
}

// TestOwnerBriefLastSentFromSettings asserts the last-sent stamp round-trips and
// returns "" when absent.
func TestOwnerBriefLastSentFromSettings(t *testing.T) {
	if got := OwnerBriefLastSentFromSettings(nil); got != "" {
		t.Errorf("nil settings must yield empty last-sent, got %q", got)
	}
	got := OwnerBriefLastSentFromSettings(map[string]interface{}{"ownerBriefLastSent": "2026-W28"})
	if got != "2026-W28" {
		t.Errorf("last-sent = %q, want 2026-W28", got)
	}
}
