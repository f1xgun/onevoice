package httpauth_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/httpauth"
)

const testSecret = "s3cr3t-internal-abcdef123456"

// nextRecorder is a downstream handler that records whether it ran, so a test
// can assert the middleware blocked a request BEFORE it reached the handler.
func nextRecorder(called *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*called = true
		w.WriteHeader(http.StatusOK)
	})
}

func TestInternalSecret_ForgedTierWithoutSecretRejected(t *testing.T) {
	var called bool
	h := httpauth.InternalSecret(testSecret)(nextRecorder(&called))

	req := httptest.NewRequest(http.MethodPost, "/chat/conv-1",
		strings.NewReader(`{"message":"hi","tier":"enterprise","business_id":"b1"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("forged tier=enterprise without secret: got status %d, want 401", rec.Code)
	}
	if called {
		t.Fatal("downstream handler ran despite missing internal secret")
	}
}

func TestInternalSecret_WrongSecretRejected(t *testing.T) {
	var called bool
	h := httpauth.InternalSecret(testSecret)(nextRecorder(&called))

	req := httptest.NewRequest(http.MethodPost, "/internal/draft-reply", strings.NewReader(`{}`))
	req.Header.Set(orchestratorclient.InternalSecretHeader, "wrong-secret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong secret: got status %d, want 401", rec.Code)
	}
	if called {
		t.Fatal("downstream handler ran despite wrong internal secret")
	}
}

func TestInternalSecret_CorrectSecretPasses(t *testing.T) {
	var called bool
	h := httpauth.InternalSecret(testSecret)(nextRecorder(&called))

	req := httptest.NewRequest(http.MethodPost, "/chat/conv-1", strings.NewReader(`{"message":"hi"}`))
	req.Header.Set(orchestratorclient.InternalSecretHeader, testSecret)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("correct secret: got status %d, want 200", rec.Code)
	}
	if !called {
		t.Fatal("downstream handler did not run with a valid internal secret")
	}
}

func TestInternalSecret_EmptySecretIsPassthrough(t *testing.T) {
	var called bool
	h := httpauth.InternalSecret("")(nextRecorder(&called))

	req := httptest.NewRequest(http.MethodPost, "/chat/conv-1", strings.NewReader(`{"tier":"enterprise"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("empty secret (dev no-op): got status %d, want 200", rec.Code)
	}
	if !called {
		t.Fatal("downstream handler did not run when the guard is disabled")
	}
}
