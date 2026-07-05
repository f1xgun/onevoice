package service

import (
	"testing"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/platform"
)

// TestComputeDrift_TitleDrift_FailOnRevert is the load-bearing drift-detection
// test. The stored side is built through platform.SyncedSnapshot (the writer's
// own formatters) with the CURRENT name "New"; the remote still shows the OLD
// name "Old". computeDrift MUST report drift on exactly the "title" field.
//
// Fail-on-revert: revert fieldDrifted/computeDrift to a no-op (always "not
// drifted") and this assertion fails — the reconciler would report every
// channel as in-sync and never surface a real divergence.
func TestComputeDrift_TitleDrift_FailOnRevert(t *testing.T) {
	b := &domain.Business{ID: uuid.New(), Name: "New", Description: "same desc"}
	stored := platform.SyncedSnapshot(b, a2a.AgentTelegram)

	remote := map[string]string{
		platform.FieldTitle:       "Old",
		platform.FieldDescription: stored[platform.FieldDescription],
	}

	drift := computeDrift(a2a.AgentTelegram, stored, remote)
	if len(drift) != 1 || drift[0] != platform.FieldTitle {
		t.Fatalf("expected drift on [title], got %v", drift)
	}
}

// TestComputeDrift_NoFalsePositives is the false-positive guard suite. Each case
// is a cosmetic/format difference OneVoice's own writer would produce, so it must
// yield NO drift. (Phone is intentionally absent: no platform reconciles phone —
// VK's groups.getById does not return it and Yandex compares schedule only — so
// there is no phone drift surface to guard.)
func TestComputeDrift_NoFalsePositives(t *testing.T) {
	longDesc := make([]rune, 400)
	for i := range longDesc {
		longDesc[i] = 'ы'
	}
	truncBiz := &domain.Business{ID: uuid.New(), Name: "Shop", Description: string(longDesc)}
	truncStored := platform.SyncedSnapshot(truncBiz, a2a.AgentTelegram)

	cases := []struct {
		name     string
		platform string
		stored   map[string]string
		remote   map[string]string
	}{
		{
			name:     "trailing_whitespace",
			platform: a2a.AgentTelegram,
			stored:   map[string]string{platform.FieldTitle: "Coffee Shop", platform.FieldDescription: "desc"},
			remote:   map[string]string{platform.FieldTitle: "Coffee Shop  ", platform.FieldDescription: "desc\n"},
		},
		{
			name:     "case_difference",
			platform: a2a.AgentTelegram,
			stored:   map[string]string{platform.FieldTitle: "Coffee Shop", platform.FieldDescription: "Best Coffee"},
			remote:   map[string]string{platform.FieldTitle: "coffee shop", platform.FieldDescription: "best coffee"},
		},
		{
			name:     "telegram_255_truncation",
			platform: a2a.AgentTelegram,
			stored:   truncStored,
			// Telegram would store exactly the truncated value the writer pushed.
			remote: map[string]string{
				platform.FieldTitle:       "Shop",
				platform.FieldDescription: truncStored[platform.FieldDescription],
			},
		},
		{
			name:     "website_scheme_and_slash",
			platform: a2a.AgentVK,
			stored:   map[string]string{platform.FieldTitle: "Shop", platform.FieldDescription: "d", platform.FieldWebsite: "https://example.com/"},
			remote:   map[string]string{platform.FieldTitle: "Shop", platform.FieldDescription: "d", platform.FieldWebsite: "example.com"},
		},
		{
			name:     "yandex_schedule_shared_boundary",
			platform: a2a.AgentYandexBusiness,
			stored:   map[string]string{platform.FieldSchedule: `{"monday":{"open":"09:00","close":"21:00"}}`},
			// Rendered, localized, day-grouped — shares the 09:00 boundary.
			remote: map[string]string{platform.FieldSchedule: "Пн-Пт 09:00–21:00"},
		},
		{
			name:     "yandex_schedule_remote_unknown",
			platform: a2a.AgentYandexBusiness,
			stored:   map[string]string{platform.FieldSchedule: `{"monday":{"open":"09:00","close":"21:00"}}`},
			remote:   map[string]string{platform.FieldSchedule: ""},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if drift := computeDrift(tc.platform, tc.stored, tc.remote); len(drift) != 0 {
				t.Errorf("expected NO drift, got %v", drift)
			}
		})
	}
}

// TestComputeDrift_RealDivergences confirms genuine changes ARE flagged, so the
// leniency above does not blind the reconciler to actual drift.
func TestComputeDrift_RealDivergences(t *testing.T) {
	cases := []struct {
		name     string
		platform string
		stored   map[string]string
		remote   map[string]string
		want     string
	}{
		{
			name:     "vk_website_changed",
			platform: a2a.AgentVK,
			stored:   map[string]string{platform.FieldTitle: "S", platform.FieldDescription: "d", platform.FieldWebsite: "example.com"},
			remote:   map[string]string{platform.FieldTitle: "S", platform.FieldDescription: "d", platform.FieldWebsite: "other.com"},
			want:     platform.FieldWebsite,
		},
		{
			name:     "yandex_schedule_disjoint_hours",
			platform: a2a.AgentYandexBusiness,
			stored:   map[string]string{platform.FieldSchedule: `{"monday":{"open":"09:00","close":"21:00"}}`},
			remote:   map[string]string{platform.FieldSchedule: "10:00–18:00"},
			want:     platform.FieldSchedule,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			drift := computeDrift(tc.platform, tc.stored, tc.remote)
			if len(drift) != 1 || drift[0] != tc.want {
				t.Fatalf("expected drift on [%s], got %v", tc.want, drift)
			}
		})
	}
}
