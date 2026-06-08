package platform

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// TestVKSyncInfo_SendsTitleFromBusinessName is the regression for the gap where
// a business rename never reached VK: groups.edit was called with
// description+phone+website but NOT title, so the community name stayed put while
// the sync task still reported "done".
func TestVKSyncInfo_SendsTitleFromBusinessName(t *testing.T) {
	var captured url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.Query()
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"response": 1})
	}))
	defer srv.Close()

	website := "https://example.com"
	b := &domain.Business{ID: uuid.New(), Name: "Кофейня", Description: "desc", Phone: "+700", Website: &website}
	integ := domain.Integration{Platform: "vk", ExternalID: "236912172"}

	err := NewVKSyncer(&fakeIntegrations{}, srv.Client(), srv.URL).SyncInfo(context.Background(), b, integ)
	require.NoError(t, err)

	assert.Equal(t, "Кофейня", captured.Get("title"), "groups.edit must carry the business name as title")
	assert.Equal(t, "desc", captured.Get("description"))
	assert.Equal(t, "236912172", captured.Get("group_id"))
}

// TestVKSyncInfo_EmptyName_OmitsTitle guards against blanking the VK community
// name when the business has no name set — title is only sent when non-empty.
func TestVKSyncInfo_EmptyName_OmitsTitle(t *testing.T) {
	var captured url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.Query()
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"response": 1})
	}))
	defer srv.Close()

	b := &domain.Business{ID: uuid.New(), Name: "", Description: "desc"}
	integ := domain.Integration{Platform: "vk", ExternalID: "g1"}

	err := NewVKSyncer(&fakeIntegrations{}, srv.Client(), srv.URL).SyncInfo(context.Background(), b, integ)
	require.NoError(t, err)

	_, hasTitle := captured["title"]
	assert.False(t, hasTitle, "empty business name must not blank the VK community title")
}
