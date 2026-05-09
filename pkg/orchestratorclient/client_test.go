package orchestratorclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStreamChat_PostsToConversationURL(t *testing.T) {
	var (
		gotMethod string
		gotPath   string
		gotBody   []byte
		gotHeader string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotHeader = r.Header.Get("X-Correlation-Id")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"done\"}\n\n"))
	}))
	defer srv.Close()

	client := New(srv.URL, srv.Client())
	resp, err := client.StreamChat(context.Background(), "conv-1", []byte(`{"message":"hi"}`), map[string]string{
		"X-Correlation-Id": "abc-123",
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/chat/conv-1" {
		t.Errorf("path = %q, want /chat/conv-1", gotPath)
	}
	if string(gotBody) != `{"message":"hi"}` {
		t.Errorf("body = %q, want JSON body forwarded verbatim", gotBody)
	}
	if gotHeader != "abc-123" {
		t.Errorf("X-Correlation-Id = %q, want abc-123", gotHeader)
	}
}

func TestStreamResume_PassesBatchIDAsQuery(t *testing.T) {
	var (
		gotPath  string
		gotQuery string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"done\"}\n\n"))
	}))
	defer srv.Close()

	client := New(srv.URL, srv.Client())
	resp, err := client.StreamResume(context.Background(), "conv-2", "batch-9", []byte(`{}`), nil)
	if err != nil {
		t.Fatalf("StreamResume: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if gotPath != "/chat/conv-2/resume" {
		t.Errorf("path = %q, want /chat/conv-2/resume", gotPath)
	}
	if !strings.Contains(gotQuery, "batch_id=batch-9") {
		t.Errorf("query = %q, want batch_id=batch-9", gotQuery)
	}
}

func TestListTools_ParsesEntries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/tools" {
			t.Errorf("path = %q, want /internal/tools", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"name":"telegram__send_channel_post","platform":"telegram","floor":"manual","editableFields":["text"],"description":"Send a Telegram channel post"},
			{"name":"vk__publish_post","platform":"vk","floor":"manual","editableFields":["text"],"description":"Publish a VK post"}
		]`))
	}))
	defer srv.Close()

	client := New(srv.URL, srv.Client())
	got, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0].Name != "telegram__send_channel_post" || got[0].Platform != "telegram" || got[0].Floor != "manual" {
		t.Errorf("got[0] = %+v, want telegram entry", got[0])
	}
	if got[1].Name != "vk__publish_post" {
		t.Errorf("got[1].Name = %q, want vk__publish_post", got[1].Name)
	}
}

func TestListToolNames_ReturnsSet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/tools/names" {
			t.Errorf("path = %q, want /internal/tools/names", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"names": []string{"telegram__send_channel_post", "vk__publish_post"},
		})
	}))
	defer srv.Close()

	client := New(srv.URL, srv.Client())
	got, err := client.ListToolNames(context.Background())
	if err != nil {
		t.Fatalf("ListToolNames: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if _, ok := got["telegram__send_channel_post"]; !ok {
		t.Error("missing telegram__send_channel_post in set")
	}
	if _, ok := got["vk__publish_post"]; !ok {
		t.Error("missing vk__publish_post in set")
	}
}

func TestNew_NilHTTPClient_DefaultsToHTTPDefaultClient(t *testing.T) {
	c := New("http://example.com/", nil)
	if c.httpClient != http.DefaultClient {
		t.Errorf("nil httpClient should fall back to http.DefaultClient; got %p (default %p)", c.httpClient, http.DefaultClient)
	}
	// Trailing slash trimmed.
	if c.baseURL != "http://example.com" {
		t.Errorf("baseURL = %q, want http://example.com (trailing slash trimmed)", c.baseURL)
	}
}

func TestDraftReply_PostsAndDecodes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/draft-reply" {
			t.Errorf("path = %q, want /internal/draft-reply", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		var got DraftReplyRequest
		_ = json.NewDecoder(r.Body).Decode(&got)
		if got.ReviewText != "hello" {
			t.Errorf("reviewText = %q, want hello", got.ReviewText)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(DraftReplyResponse{DraftReply: "thanks", Provider: "openrouter"})
	}))
	defer srv.Close()

	client := New(srv.URL, srv.Client())
	got, err := client.DraftReply(context.Background(), DraftReplyRequest{
		BusinessID: "b1", BusinessName: "Cafe", Platform: "vk", ReviewText: "hello", Rating: 5,
	})
	if err != nil {
		t.Fatalf("DraftReply: %v", err)
	}
	if got.DraftReply != "thanks" {
		t.Errorf("DraftReply = %q, want thanks", got.DraftReply)
	}
	if got.Provider != "openrouter" {
		t.Errorf("Provider = %q, want openrouter", got.Provider)
	}
}
