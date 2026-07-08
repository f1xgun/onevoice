package connhealth

import (
	"testing"
	"time"
)

// TestHealthPatch_TouchesOnlyHealthKey is the clobber-safety guard: the health
// write patch must contain ONLY the connection_health key, so the targeted
// jsonb_set that persists it can never rewrite a sibling metadata key
// (telegram_user_id, channel_title, access_verified) on the same row.
func TestHealthPatch_TouchesOnlyHealthKey(t *testing.T) {
	existing := map[string]interface{}{
		"telegram_user_id": "555",
		"channel_title":    "Кафе",
		"access_verified":  true,
	}
	patch := HealthPatch(existing, Result{Status: StatusBroken, ReasonCode: ReasonTelegramNotAdmin, CheckedAt: time.Now()})

	if len(patch) != 1 {
		t.Fatalf("health patch must touch exactly one metadata key, got %d: %v", len(patch), patch)
	}
	sub, ok := patch[MetadataKey].(map[string]interface{})
	if !ok {
		t.Fatalf("health patch must set only the %q sub-object", MetadataKey)
	}
	if sub["status"] != string(StatusBroken) {
		t.Fatalf("expected the broken verdict in the patch, got %v", sub["status"])
	}
	for _, sibling := range []string{"telegram_user_id", "channel_title", "access_verified"} {
		if _, present := patch[sibling]; present {
			t.Fatalf("health patch must NOT include sibling key %q (would clobber it)", sibling)
		}
	}
}

// TestHealthPatch_PreservesPriorNudgedAt: a routine health write must carry the
// existing owner-nudge throttle stamp forward so the jsonb_set (which replaces
// the whole connection_health object) does not drop it.
func TestHealthPatch_PreservesPriorNudgedAt(t *testing.T) {
	nudgedAt := time.Date(2026, 7, 7, 9, 0, 0, 0, time.UTC)
	existing := MergeNudgedAt(
		MergeIntoMetadata(nil, Result{Status: StatusBroken, ReasonCode: ReasonYandexSessionExpiry, CheckedAt: nudgedAt.Add(-time.Hour)}),
		nudgedAt,
	)
	patch := HealthPatch(existing, Result{Status: StatusBroken, ReasonCode: ReasonYandexSessionExpiry, CheckedAt: time.Now()})
	sub := patch[MetadataKey].(map[string]interface{})
	if sub["nudged_at"] != nudgedAt.UTC().Format(time.RFC3339) {
		t.Fatalf("health patch dropped the prior nudged_at throttle stamp, got %v", sub["nudged_at"])
	}
}

func TestDemoteOnlyIfConclusive_UnknownNeverOverwritesActive(t *testing.T) {
	checkedAt := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	prev := Result{Status: StatusActive, ReasonCode: ReasonOK, CheckedAt: checkedAt.Add(-time.Hour)}
	next := Result{Status: StatusUnknown, ReasonCode: ReasonInconclusive, CheckedAt: checkedAt}

	got := DemoteOnlyIfConclusive(prev, next)

	if got.Status != StatusActive {
		t.Fatalf("expected fail-soft to keep prior active, got %q", got.Status)
	}
	if got.ReasonCode != ReasonOK {
		t.Fatalf("expected prior reason preserved, got %q", got.ReasonCode)
	}
	if !got.CheckedAt.Equal(checkedAt) {
		t.Fatalf("expected checked_at to advance to the new probe time, got %v", got.CheckedAt)
	}
}

func TestDemoteOnlyIfConclusive_ConclusiveWins(t *testing.T) {
	prev := Result{Status: StatusActive, ReasonCode: ReasonOK}
	next := Result{Status: StatusBroken, ReasonCode: ReasonTelegramNotAdmin, CheckedAt: time.Now()}

	got := DemoteOnlyIfConclusive(prev, next)
	if got.Status != StatusBroken || got.ReasonCode != ReasonTelegramNotAdmin {
		t.Fatalf("expected conclusive broken to win, got %+v", got)
	}
}

func TestDemoteOnlyIfConclusive_NoPriorRecordsUnknown(t *testing.T) {
	next := Result{Status: StatusUnknown, ReasonCode: ReasonInconclusive, CheckedAt: time.Now()}
	got := DemoteOnlyIfConclusive(Result{}, next)
	if got.Status != StatusUnknown {
		t.Fatalf("expected first-ever unknown to be recorded, got %q", got.Status)
	}
}

func TestMergeIntoMetadata_PreservesSiblingKeysAndNudgedAt(t *testing.T) {
	nudgedAt := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	existing := map[string]interface{}{
		"channel_title":       "Acme News",
		"linked_group_status": "ok",
		MetadataKey: map[string]interface{}{
			"status":     string(StatusBroken),
			"nudged_at":  nudgedAt.Format(time.RFC3339),
			"checked_at": nudgedAt.Format(time.RFC3339),
		},
	}
	res := Result{Status: StatusActive, ReasonCode: ReasonOK, CheckedAt: time.Now()}

	out := MergeIntoMetadata(existing, res)

	if out["channel_title"] != "Acme News" {
		t.Fatalf("expected channel_title preserved, got %v", out["channel_title"])
	}
	if out["linked_group_status"] != "ok" {
		t.Fatalf("expected linked_group_status preserved, got %v", out["linked_group_status"])
	}
	sub, ok := out[MetadataKey].(map[string]interface{})
	if !ok {
		t.Fatalf("expected connection_health sub-object, got %T", out[MetadataKey])
	}
	if sub["status"] != string(StatusActive) {
		t.Fatalf("expected new status written, got %v", sub["status"])
	}
	if _, ok := sub["nudged_at"]; !ok {
		t.Fatalf("expected nudged_at preserved across a health write")
	}
	if _, mutated := existing[MetadataKey].(map[string]interface{})["reason_code"]; mutated {
		t.Fatalf("MergeIntoMetadata must not mutate the input map")
	}
}

func TestMergeNudgedAt_SetAndClear(t *testing.T) {
	base := MergeIntoMetadata(nil, Result{Status: StatusBroken, ReasonCode: ReasonYandexSessionExpiry, CheckedAt: time.Now()})
	stamped := time.Date(2026, 7, 7, 8, 0, 0, 0, time.UTC)

	withNudge := MergeNudgedAt(base, stamped)
	if got := ReadNudgedAt(withNudge); !got.Equal(stamped) {
		t.Fatalf("expected nudged_at stamped, got %v", got)
	}
	if sub, _ := withNudge[MetadataKey].(map[string]interface{}); sub["status"] != string(StatusBroken) {
		t.Fatalf("expected status preserved through nudge stamp, got %v", sub["status"])
	}

	cleared := MergeNudgedAt(withNudge, time.Time{})
	if got := ReadNudgedAt(cleared); !got.IsZero() {
		t.Fatalf("expected nudged_at cleared on recovery, got %v", got)
	}
}

func TestReadFromMetadata_RoundTrip(t *testing.T) {
	checkedAt := time.Date(2026, 7, 7, 10, 30, 0, 0, time.UTC)
	meta := MergeIntoMetadata(nil, Result{Status: StatusBroken, ReasonCode: ReasonVKWallScopeMissing, CheckedAt: checkedAt})

	got := ReadFromMetadata(meta)
	if got.Status != StatusBroken || got.ReasonCode != ReasonVKWallScopeMissing {
		t.Fatalf("unexpected round-trip: %+v", got)
	}
	if !got.CheckedAt.Equal(checkedAt) {
		t.Fatalf("expected checked_at round-trip, got %v", got.CheckedAt)
	}
}

func TestReadFromMetadata_AbsentIsZero(t *testing.T) {
	if got := ReadFromMetadata(map[string]interface{}{"channel_title": "x"}); got.Status != "" {
		t.Fatalf("expected empty status when absent, got %q", got.Status)
	}
	if got := ReadFromMetadata(nil); got.Status != "" {
		t.Fatalf("expected empty status for nil metadata, got %q", got.Status)
	}
}
