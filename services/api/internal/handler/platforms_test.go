package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

func TestPlatformsHandler_AllConfigured(t *testing.T) {
	h := NewPlatformsHandler(PlatformAvailability{
		Telegram:       true,
		VK:             true,
		YandexBusiness: true,
		GoogleBusiness: true,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/platforms", http.NoBody)
	w := httptest.NewRecorder()
	h.List(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var got []domain.Platform
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Len(t, got, 7)

	statuses := map[string]domain.PlatformStatus{}
	for _, p := range got {
		statuses[p.ID] = p.Status
	}
	assert.Equal(t, domain.PlatformStatusActive, statuses["telegram"])
	assert.Equal(t, domain.PlatformStatusActive, statuses["vk"])
	assert.Equal(t, domain.PlatformStatusActive, statuses["yandex_business"])
	assert.Equal(t, domain.PlatformStatusComingSoon, statuses["google_business"])
	assert.Equal(t, domain.PlatformStatusComingSoon, statuses["2gis"])
	assert.Equal(t, domain.PlatformStatusComingSoon, statuses["avito"])
	assert.Equal(t, domain.PlatformStatusComingSoon, statuses["whatsapp"])
}

func TestPlatformsHandler_DowngradesMissingCreds(t *testing.T) {
	h := NewPlatformsHandler(PlatformAvailability{
		Telegram:       false,
		VK:             true,
		YandexBusiness: true,
		GoogleBusiness: false,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/platforms", http.NoBody)
	w := httptest.NewRecorder()
	h.List(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var got []domain.Platform
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))

	statuses := map[string]domain.PlatformStatus{}
	for _, p := range got {
		statuses[p.ID] = p.Status
	}
	assert.Equal(t, domain.PlatformStatusOAuthNotConfigured, statuses["telegram"])
	assert.Equal(t, domain.PlatformStatusActive, statuses["vk"])
	assert.Equal(t, domain.PlatformStatusActive, statuses["yandex_business"])
	assert.Equal(t, domain.PlatformStatusComingSoon, statuses["google_business"])
}

func TestPlatformsHandler_ComingSoonNeverDowngraded(t *testing.T) {
	h := NewPlatformsHandler(PlatformAvailability{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/platforms", http.NoBody)
	w := httptest.NewRecorder()
	h.List(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var got []domain.Platform
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))

	statuses := map[string]domain.PlatformStatus{}
	for _, p := range got {
		statuses[p.ID] = p.Status
	}
	assert.Equal(t, domain.PlatformStatusComingSoon, statuses["2gis"])
	assert.Equal(t, domain.PlatformStatusComingSoon, statuses["avito"])
	assert.Equal(t, domain.PlatformStatusComingSoon, statuses["whatsapp"])
	assert.Equal(t, domain.PlatformStatusOAuthNotConfigured, statuses["telegram"])
}

func TestPlatformsHandler_PreservesDisplayOrder(t *testing.T) {
	h := NewPlatformsHandler(PlatformAvailability{
		Telegram: true, VK: true, YandexBusiness: true, GoogleBusiness: true,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/platforms", http.NoBody)
	w := httptest.NewRecorder()
	h.List(w, req)

	var got []domain.Platform
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))

	wantOrder := []string{
		"telegram",
		"vk",
		"yandex_business",
		"google_business",
		"2gis",
		"avito",
		"whatsapp",
	}
	for i, p := range got {
		assert.Equal(t, wantOrder[i], p.ID, "position %d", i)
	}
}

// TestPlatformsHandler_WireOmitsNameAndDescription pins the i18n Phase C2
// invariant: the JSON response does NOT include the `name` and `description`
// fields. The frontend resolves both via its messages/*.json bundles
// (platforms.fullLabel.<id>, platforms.description.<id>). Re-introducing
// either field on the wire would silently revert clients that have
// migrated to the i18n flow.
func TestPlatformsHandler_WireOmitsNameAndDescription(t *testing.T) {
	h := NewPlatformsHandler(PlatformAvailability{
		Telegram: true, VK: true, YandexBusiness: true, GoogleBusiness: true,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/platforms", http.NoBody)
	w := httptest.NewRecorder()
	h.List(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var raw []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	require.NotEmpty(t, raw)
	for _, entry := range raw {
		_, hasName := entry["name"]
		assert.False(t, hasName, "platform %v: response must not include `name` key", entry["id"])
		_, hasDesc := entry["description"]
		assert.False(t, hasDesc, "platform %v: response must not include `description` key", entry["id"])
		assert.Contains(t, entry, "id")
		assert.Contains(t, entry, "status")
	}
}
