package gbp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_ListAccounts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "/v1/accounts", r.URL.Path)
		resp := ListAccountsResponse{
			Accounts: []Account{
				{Name: "accounts/123", AccountName: "Test Business", Type: "PERSONAL"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := New("test-token")
	c.AccountsBaseURL = srv.URL

	accounts, err := c.ListAccounts(context.Background())
	require.NoError(t, err)
	require.Len(t, accounts, 1)
	assert.Equal(t, "accounts/123", accounts[0].Name)
	assert.Equal(t, "Test Business", accounts[0].AccountName)
}

func TestClient_ListLocations(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Contains(t, r.URL.Path, "/v1/accounts/123/locations")
		assert.Equal(t, "name,title,storefrontAddress", r.URL.Query().Get("readMask"))
		resp := ListLocationsResponse{
			Locations: []Location{
				{Name: "locations/456", Title: "My Shop"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := New("test-token")
	c.BusinessInfoBaseURL = srv.URL

	locations, err := c.ListLocations(context.Background(), "accounts/123")
	require.NoError(t, err)
	require.Len(t, locations, 1)
	assert.Equal(t, "locations/456", locations[0].Name)
	assert.Equal(t, "My Shop", locations[0].Title)
}

func TestClient_GetReviews(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "/v4/accounts/1/locations/2/reviews", r.URL.Path)
		assert.Equal(t, "10", r.URL.Query().Get("pageSize"))

		resp := ListReviewsResponse{
			Reviews: []Review{
				{
					ReviewID:   "rev-1",
					Name:       "accounts/1/locations/2/reviews/rev-1",
					Reviewer:   Reviewer{DisplayName: "Alice"},
					StarRating: "FIVE",
					Comment:    "Excellent!",
					CreateTime: "2026-01-01T00:00:00Z",
				},
			},
			AverageRating:    5.0,
			TotalReviewCount: 1,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := New("test-token")
	c.ReviewsBaseURL = srv.URL

	resp, err := c.GetReviews(context.Background(), "accounts/1/locations/2", 10)
	require.NoError(t, err)
	require.Len(t, resp.Reviews, 1)
	assert.Equal(t, "rev-1", resp.Reviews[0].ReviewID)
	assert.Equal(t, "FIVE", resp.Reviews[0].StarRating)
	assert.Equal(t, 5.0, resp.AverageRating)
	assert.Equal(t, 1, resp.TotalReviewCount)
}

func TestClient_GetReviews_DefaultLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "50", r.URL.Query().Get("pageSize"))
		json.NewEncoder(w).Encode(ListReviewsResponse{})
	}))
	defer srv.Close()

	c := New("test-token")
	c.ReviewsBaseURL = srv.URL

	_, err := c.GetReviews(context.Background(), "accounts/1/locations/2", 0)
	require.NoError(t, err)
}

func TestClient_ReplyReview(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/v4/accounts/1/locations/2/reviews/rev-1/reply", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		body, _ := io.ReadAll(r.Body)
		var payload map[string]string
		json.Unmarshal(body, &payload)
		assert.Equal(t, "Thank you!", payload["comment"])

		json.NewEncoder(w).Encode(ReviewReply{
			Comment:    "Thank you!",
			UpdateTime: "2026-01-05T00:00:00Z",
		})
	}))
	defer srv.Close()

	c := New("test-token")
	c.ReviewsBaseURL = srv.URL

	reply, err := c.ReplyReview(context.Background(), "accounts/1/locations/2/reviews/rev-1", "Thank you!")
	require.NoError(t, err)
	assert.Equal(t, "Thank you!", reply.Comment)
	assert.Equal(t, "2026-01-05T00:00:00Z", reply.UpdateTime)
}

func TestClient_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
				Status  string `json:"status"`
			}{Code: 403, Message: "API not enabled", Status: "PERMISSION_DENIED"},
		})
	}))
	defer srv.Close()

	c := New("test-token")
	c.AccountsBaseURL = srv.URL

	_, err := c.ListAccounts(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "google api error")
	assert.Contains(t, err.Error(), "403")
}
