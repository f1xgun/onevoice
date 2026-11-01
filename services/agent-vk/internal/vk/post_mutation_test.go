package vk_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/services/agent-vk/internal/vk"
)

func TestPostMutationsHTTP(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(fmt.Sprint("delete=", remove), func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				calls++
				require.NoError(t, r.ParseForm())
				require.Equal(t, "Bearer user-token", r.Header.Get("Authorization"))
				switch r.URL.Path {
				case "/wall.getById":
					require.Equal(t, "-123_42", r.FormValue("posts"))
					_, _ = fmt.Fprint(w, `{"response":{"items":[{"id":42,"owner_id":-123,"attachments":[{"type":"photo","photo":{"id":7,"owner_id":-123,"access_key":"key"}}]}]}}`)
				case "/wall.edit":
					require.Equal(t, "-123", r.FormValue("owner_id"))
					require.Equal(t, "42", r.FormValue("post_id"))
					require.Equal(t, "New caption", r.FormValue("message"))
					require.Equal(t, "photo-123_7_key", r.FormValue("attachments"))
					_, _ = fmt.Fprint(w, `{"response":{"post_id":42}}`)
				case "/wall.delete":
					require.Equal(t, "-123", r.FormValue("owner_id"))
					require.Equal(t, "42", r.FormValue("post_id"))
					_, _ = fmt.Fprint(w, `{"response":1}`)
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
				}
			}))
			defer srv.Close()
			client := vk.NewWithBaseURL("user-token", srv.URL+"/")
			if remove {
				require.NoError(t, client.DeletePost("-123", 42))
				require.Equal(t, 1, calls)
			} else {
				require.NoError(t, client.EditPost("-123", 42, "New caption"))
				require.Equal(t, 2, calls)
			}
		})
	}
}

func TestEditPostRefusesUnknownMediaOrForeignReceipt(t *testing.T) {
	for _, post := range []string{
		`{"id":42,"owner_id":-123,"attachments":[{"type":"video"}]}`,
		`{"id":42,"owner_id":-999}`,
		`{"id":43,"owner_id":-123}`,
	} {
		t.Run(post, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				require.Equal(t, "/wall.getById", r.URL.Path, "must not edit after invalid receipt")
				_, _ = fmt.Fprintf(w, `{"response":{"items":[%s]}}`, post)
			}))
			defer srv.Close()
			require.Error(t, vk.NewWithBaseURL("user-token", srv.URL+"/").EditPost("-123", 42, "Updated"))
		})
	}
}

func TestPostMutationVKErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, vkError(15, "Access denied"))
	}))
	defer srv.Close()
	client := vk.NewWithBaseURL("user-token", srv.URL+"/")
	require.ErrorContains(t, client.EditPost("-123", 42, "Updated"), "Access denied")
	require.ErrorContains(t, client.DeletePost("-123", 42), "Access denied")
}

func TestDeletePostRequiresConfirmation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"response":0}`)
	}))
	defer srv.Close()
	require.ErrorContains(t, vk.NewWithBaseURL("user-token", srv.URL+"/").DeletePost("-123", 42), "not confirmed")
}
