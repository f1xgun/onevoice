package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// stubSharedSetter records the SetSharedSession call.
type stubSharedSetter struct {
	called bool
	params service.SharedSessionParams
	result *domain.Integration
	err    error
}

func (s *stubSharedSetter) SetSharedSession(_ context.Context, params service.SharedSessionParams) (*domain.Integration, error) {
	s.called = true
	s.params = params
	return s.result, s.err
}

// TestInternalSharedSession_NotConfigured_FailsClosed: with the sentinel unset,
// the bootstrap endpoint returns 503 and never calls the service.
func TestInternalSharedSession_NotConfigured_FailsClosed(t *testing.T) {
	setter := &stubSharedSetter{}
	h := NewInternalYandexSharedHandler(setter, "")

	req := httptest.NewRequest(http.MethodPost, "/internal/v1/yandex/shared-session",
		strings.NewReader(`{"cookies":"Session_id=3:1.2.3; sessionid2=x"}`))
	rr := httptest.NewRecorder()
	h.SetSharedSession(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when sentinel unset, got %d", rr.Code)
	}
	if setter.called {
		t.Fatal("service must NOT be called when the delegated plane is unconfigured")
	}
}

// TestInternalSharedSession_InvalidSentinel_Fails: a non-uuid sentinel is a
// misconfiguration surfaced as 503.
func TestInternalSharedSession_InvalidSentinel_Fails(t *testing.T) {
	setter := &stubSharedSetter{}
	h := NewInternalYandexSharedHandler(setter, "not-a-uuid")

	req := httptest.NewRequest(http.MethodPost, "/internal/v1/yandex/shared-session",
		strings.NewReader(`{"cookies":"Session_id=3:1.2.3"}`))
	rr := httptest.NewRecorder()
	h.SetSharedSession(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for non-uuid sentinel, got %d", rr.Code)
	}
	if setter.called {
		t.Fatal("service must NOT be called with a misconfigured sentinel")
	}
}

// TestInternalSharedSession_Stores: a valid cookie payload is stored under the
// sentinel business.
func TestInternalSharedSession_Stores(t *testing.T) {
	sentinel := uuid.New()
	integrationID := uuid.New()
	setter := &stubSharedSetter{result: &domain.Integration{ID: integrationID, BusinessID: sentinel}}
	h := NewInternalYandexSharedHandler(setter, sentinel.String())

	body := `{"cookies":"Session_id=3:1701234567.5.0.1701234567890:Vqx9aBCDeFgHiJkLmNoPq:1.1|abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGH; sessionid2=3:abc"}`
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/yandex/shared-session", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.SetSharedSession(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	if !setter.called {
		t.Fatal("service must be called with a valid payload")
	}
	if setter.params.SharedBusinessID != sentinel {
		t.Fatalf("must store under the sentinel business, got %s", setter.params.SharedBusinessID)
	}
	if setter.params.Credential == "" {
		t.Fatal("credential must be forwarded to the service")
	}
}
