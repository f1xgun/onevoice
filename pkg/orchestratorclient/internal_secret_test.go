package orchestratorclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWithInternalSecret_InjectsHeader(t *testing.T) {
	var gotSecret string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSecret = r.Header.Get(InternalSecretHeader)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := WithInternalSecret(&http.Client{}, "top-secret-value-123456")
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, http.NoBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if gotSecret != "top-secret-value-123456" {
		t.Fatalf("server saw %q, want the injected secret", gotSecret)
	}
	if req.Header.Get(InternalSecretHeader) != "" {
		t.Fatal("caller's original request was mutated; RoundTrip must clone")
	}
}

func TestWithInternalSecret_EmptySecretReturnsBase(t *testing.T) {
	base := &http.Client{}
	if got := WithInternalSecret(base, ""); got != base {
		t.Fatal("empty secret should return the base client unchanged")
	}
	if got := WithInternalSecret(nil, ""); got != http.DefaultClient {
		t.Fatal("empty secret with nil base should return http.DefaultClient")
	}
}
