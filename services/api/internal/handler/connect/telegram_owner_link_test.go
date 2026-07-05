package connect

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
)

// fakeOwnerLinkMinter is a minimal TelegramOwnerLinkMinter for the handler test.
type fakeOwnerLinkMinter struct {
	enabled    bool
	startURL   string
	mintErr    error
	mintedBiz  uuid.UUID
	mintCalled bool
}

func (f *fakeOwnerLinkMinter) Enabled() bool { return f.enabled }

func (f *fakeOwnerLinkMinter) Mint(_ context.Context, businessID uuid.UUID) (string, error) {
	f.mintCalled = true
	f.mintedBiz = businessID
	if f.mintErr != nil {
		return "", f.mintErr
	}
	return f.startURL, nil
}

func newOwnerLinkHandler(t *testing.T, minter TelegramOwnerLinkMinter) *ConnectHandler {
	t.Helper()
	return NewConnectHandler(new(MockConnectIntegrationService), new(MockBusinessService), minter, ConnectConfig{}, nil)
}

// TestStartTelegramOwnerLink_Authorized_ReturnsStartURL: an authenticated admin
// of the business with PermIntegrationsConnect gets a 200 with the start URL, and
// the minted business id comes from BusinessContext (never request input).
func TestStartTelegramOwnerLink_Authorized_ReturnsStartURL(t *testing.T) {
	bizID := uuid.New()
	userID := uuid.New()
	minter := &fakeOwnerLinkMinter{enabled: true, startURL: "https://t.me/onevoice_bot?start=abcDEF123-_"}
	h := newOwnerLinkHandler(t, minter)

	req := httptest.NewRequest(http.MethodPost, "/oauth/telegram/owner-link", http.NoBody)
	req = req.WithContext(connectBizCtx(bizID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()

	h.StartTelegramOwnerLink(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp openapi.TelegramOwnerLinkResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.StartUrl != minter.startURL {
		t.Fatalf("start URL mismatch: got %q", resp.StartUrl)
	}
	if resp.ExpiresInSeconds <= 0 {
		t.Fatalf("expires_in_seconds must be positive, got %d", resp.ExpiresInSeconds)
	}
	if !minter.mintCalled || minter.mintedBiz != bizID {
		t.Fatalf("mint must be called with the BusinessContext business id (%s), got %s", bizID, minter.mintedBiz)
	}
}

// TestStartTelegramOwnerLink_NoBusinessContext_500: absent BusinessContext
// (middleware misconfiguration) is a 500, never a mint.
func TestStartTelegramOwnerLink_NoBusinessContext_500(t *testing.T) {
	minter := &fakeOwnerLinkMinter{enabled: true}
	h := newOwnerLinkHandler(t, minter)

	req := httptest.NewRequest(http.MethodPost, "/oauth/telegram/owner-link", http.NoBody)
	rr := httptest.NewRecorder()
	h.StartTelegramOwnerLink(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 without BusinessContext, got %d", rr.Code)
	}
	if minter.mintCalled {
		t.Fatal("must not mint without a BusinessContext")
	}
}

// TestStartTelegramOwnerLink_MissingPerm_403: a member lacking
// PermIntegrationsConnect is forbidden — mirrors ConnectTelegram authz.
func TestStartTelegramOwnerLink_MissingPerm_403(t *testing.T) {
	minter := &fakeOwnerLinkMinter{enabled: true}
	h := newOwnerLinkHandler(t, minter)

	req := httptest.NewRequest(http.MethodPost, "/oauth/telegram/owner-link", http.NoBody)
	// BusinessContext with NO permissions.
	req = req.WithContext(connectBizCtx(uuid.New(), uuid.New()))
	rr := httptest.NewRecorder()
	h.StartTelegramOwnerLink(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without PermIntegrationsConnect, got %d", rr.Code)
	}
	if minter.mintCalled {
		t.Fatal("must not mint without PermIntegrationsConnect")
	}
}

// TestStartTelegramOwnerLink_Disabled_404: when the handshake is unconfigured
// (no bot username) the endpoint returns 404 and never mints a dead link.
func TestStartTelegramOwnerLink_Disabled_404(t *testing.T) {
	minter := &fakeOwnerLinkMinter{enabled: false}
	h := newOwnerLinkHandler(t, minter)

	req := httptest.NewRequest(http.MethodPost, "/oauth/telegram/owner-link", http.NoBody)
	req = req.WithContext(connectBizCtx(uuid.New(), uuid.New(), authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()
	h.StartTelegramOwnerLink(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when handshake disabled, got %d", rr.Code)
	}
	if minter.mintCalled {
		t.Fatal("must not mint when the handshake is disabled")
	}
}

// TestStartTelegramOwnerLink_NilMinter_404: a nil minter (owner-link service not
// wired) also returns 404 fail-closed without panicking.
func TestStartTelegramOwnerLink_NilMinter_404(t *testing.T) {
	h := newOwnerLinkHandler(t, nil)

	req := httptest.NewRequest(http.MethodPost, "/oauth/telegram/owner-link", http.NoBody)
	req = req.WithContext(connectBizCtx(uuid.New(), uuid.New(), authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()
	h.StartTelegramOwnerLink(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 with a nil minter, got %d", rr.Code)
	}
}

// TestStartTelegramOwnerLink_MintError_500: a mint failure is a 500, not a leak.
func TestStartTelegramOwnerLink_MintError_500(t *testing.T) {
	minter := &fakeOwnerLinkMinter{enabled: true, mintErr: errors.New("db down")}
	h := newOwnerLinkHandler(t, minter)

	req := httptest.NewRequest(http.MethodPost, "/oauth/telegram/owner-link", http.NoBody)
	req = req.WithContext(connectBizCtx(uuid.New(), uuid.New(), authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()
	h.StartTelegramOwnerLink(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on mint failure, got %d", rr.Code)
	}
}
