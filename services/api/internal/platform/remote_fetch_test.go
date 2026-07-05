package platform

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// TestTelegramFetchRemote_ParsesGetChat proves the Telegram RemoteFetcher reads
// title + description from a getChat ok:true envelope, keyed by the shared
// Field* constants. The mock server is injected through the telegramBase
// constructor override.
func TestTelegramFetchRemote_ParsesGetChat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true,
			"result": map[string]interface{}{
				"title":       "Coffee Shop",
				"description": "Best coffee in town",
			},
		})
	}))
	defer srv.Close()

	b := &domain.Business{ID: uuid.New(), Name: "Coffee Shop"}
	integ := domain.Integration{Platform: "telegram", ExternalID: "@coffee"}

	snap, err := NewTelegramSyncer(&fakeIntegrations{}, srv.Client(), srv.URL, "").
		FetchRemote(context.Background(), b, integ)
	require.NoError(t, err)
	assert.Empty(t, snap.Err)
	assert.Equal(t, "Coffee Shop", snap.Fields[FieldTitle])
	assert.Equal(t, "Best coffee in town", snap.Fields[FieldDescription])
}

// TestTelegramFetchRemote_NotOKIsUnknown proves a Telegram ok:false envelope
// surfaces as RemoteSnapshot.Err (unknown), NOT as a Go error and NOT as drift.
func TestTelegramFetchRemote_NotOKIsUnknown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":          false,
			"description": "Bad Request: chat not found",
		})
	}))
	defer srv.Close()

	snap, err := NewTelegramSyncer(&fakeIntegrations{}, srv.Client(), srv.URL, "").
		FetchRemote(context.Background(), &domain.Business{ID: uuid.New()}, domain.Integration{Platform: "telegram", ExternalID: "@x"})
	require.NoError(t, err)
	assert.Equal(t, "Bad Request: chat not found", snap.Err)
	assert.Nil(t, snap.Fields)
}

// TestVKFetchRemote_ParsesGroupsGetById proves the VK RemoteFetcher reads
// name + description + site (website) from groups.getById. The mock server is
// injected through the vkBase constructor override.
func TestVKFetchRemote_ParsesGroupsGetById(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"response": map[string]interface{}{
				"groups": []map[string]interface{}{
					{"name": "Кофейня", "description": "лучший кофе", "site": "https://example.com/"},
				},
			},
		})
	}))
	defer srv.Close()

	b := &domain.Business{ID: uuid.New(), Name: "Кофейня"}
	integ := domain.Integration{Platform: "vk", ExternalID: "236912172"}

	snap, err := NewVKSyncer(&fakeIntegrations{}, srv.Client(), srv.URL).
		FetchRemote(context.Background(), b, integ)
	require.NoError(t, err)
	assert.Empty(t, snap.Err)
	assert.Equal(t, "Кофейня", snap.Fields[FieldTitle])
	assert.Equal(t, "лучший кофе", snap.Fields[FieldDescription])
	assert.Equal(t, "https://example.com/", snap.Fields[FieldWebsite])
}

// TestVKFetchRemote_APIErrorIsUnknown proves a VK error envelope surfaces as
// RemoteSnapshot.Err (unknown), not a Go error.
func TestVKFetchRemote_APIErrorIsUnknown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{"error_code": 5, "error_msg": "User authorization failed: access_token has expired."},
		})
	}))
	defer srv.Close()

	snap, err := NewVKSyncer(&fakeIntegrations{}, srv.Client(), srv.URL).
		FetchRemote(context.Background(), &domain.Business{ID: uuid.New()}, domain.Integration{Platform: "vk", ExternalID: "g1"})
	require.NoError(t, err)
	assert.Contains(t, snap.Err, "access_token has expired")
	assert.Nil(t, snap.Fields)
}

// TestSyncedSnapshot_TelegramTruncates proves the stored side is built through
// the SAME formatter the writer uses — including the 255-rune truncation — so a
// value we just pushed reads back identical.
func TestSyncedSnapshot_TelegramTruncates(t *testing.T) {
	long := make([]rune, 400)
	for i := range long {
		long[i] = 'ы'
	}
	b := &domain.Business{ID: uuid.New(), Name: "Shop", Description: string(long)}

	stored := SyncedSnapshot(b, "telegram")
	assert.Equal(t, "Shop", stored[FieldTitle])
	assert.LessOrEqual(t, len([]rune(stored[FieldDescription])), 255,
		"stored Telegram description must be built through formatTelegramDescription's 255-rune truncation")
	// The truncation is exactly what setChatDescription would store, so it equals
	// the write path's own output.
	assert.Equal(t, formatTelegramDescription(b), stored[FieldDescription])
}

// TestSyncedSnapshot_TelegramHonorsTemplate proves the stored side renders a
// per-business descriptionTemplate the SAME way the write path
// (SyncDescription → renderBusinessDescription) does. Without this, a business
// with a custom template would show permanent false-positive "description" drift
// because the stored side used the default formatter while the live channel
// carries the rendered template.
func TestSyncedSnapshot_TelegramHonorsTemplate(t *testing.T) {
	site := "example.com"
	b := &domain.Business{
		ID:      uuid.New(),
		Name:    "Кофейня",
		Phone:   "+7 900 000-00-00",
		Website: &site,
		Settings: map[string]interface{}{
			DescriptionTemplateSettingsKey: "{name} · {phone} · {website}",
		},
	}

	stored := SyncedSnapshot(b, "telegram")

	assert.Equal(t, renderBusinessDescription(b, maxTelegramDescription), stored[FieldDescription],
		"stored side must be built through the write path's renderBusinessDescription")
	assert.Equal(t, "Кофейня · +7 900 000-00-00 · example.com", stored[FieldDescription],
		"stored description must equal the rendered template, not the default formatter")
	assert.NotEqual(t, formatTelegramDescription(b), stored[FieldDescription],
		"a custom template must diverge from the default formatter, else the regression is masked")
}
