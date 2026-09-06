package connect

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"

	"github.com/f1xgun/onevoice/pkg/i18n"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/language"
)

func TestTelegramErrorsLocalizedAndStable(t *testing.T) {
	for _, tc := range []struct {
		name   string
		kind   telegramErrKind
		status int
		key    string
	}{
		{"api_rejected", telegramErrAPIRejected, 400, "connect.telegram.channel_unavailable"},
		{"forbidden", telegramErrForbidden, 403, "connect.telegram.no_access"},
		{"rate_limited", telegramErrRateLimited, 429, "connect.telegram.rate_limited"},
		{"unreachable", telegramErrUnreachable, 502, "connect.telegram.unreachable"},
	} {
		for _, locale := range []language.Tag{language.Russian, language.English} {
			t.Run(tc.name+locale.String(), func(t *testing.T) {
				req := httptest.NewRequest(http.MethodPost, "/oauth/telegram/connect", http.NoBody)
				req = req.WithContext(i18n.WithLocale(req.Context(), locale))
				rr := httptest.NewRecorder()
				writeTelegramAPIError(rr, req, &telegramAPIError{Kind: tc.kind, Description: "untrusted upstream detail", Err: context.DeadlineExceeded}, "connect.telegram.connect_failed")
				require.Equal(t, tc.status, rr.Code)
				var response openapi.TelegramConnectError
				require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &response))
				assert.Equal(t, tc.name, response.Reason)
				assert.Equal(t, i18n.Tr(req.Context(), tc.key), response.Error)
				assert.NotEqual(t, tc.key, response.Error)
				assert.NotContains(t, rr.Body.String(), "untrusted")
			})
		}
	}
}

func TestTelegramConnectRightsReasons(t *testing.T) {
	for _, tc := range []struct{ membership, reason, key string }{
		{"member", "not_admin", "connect.telegram.not_admin"},
		{"administrator", "no_post_rights", "connect.telegram.no_post_rights"},
	} {
		for _, locale := range []language.Tag{language.Russian, language.English} {
			t.Run(tc.reason+locale.String(), func(t *testing.T) {
				srv := telegramConnectMemberMock(t, tc.membership, false)
				defer srv.Close()
				integrations := new(MockConnectIntegrationService)
				h := NewConnectHandler(integrations, new(MockBusinessService), nil, ConnectConfig{TelegramBotToken: "test-bot", telegramAPIBaseURL: srv.URL}, srv.Client())
				req := httptest.NewRequest(http.MethodPost, "/oauth/telegram/connect", strings.NewReader(`{"channel_id":"@channel"}`))
				req = req.WithContext(i18n.WithLocale(connectBizCtx(uuid.New(), uuid.New(), authz.PermIntegrationsConnect), locale))
				rr := httptest.NewRecorder()
				h.ConnectTelegram(rr, req)
				require.Equal(t, http.StatusConflict, rr.Code)
				var response openapi.TelegramConnectError
				require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &response))
				assert.Equal(t, tc.reason, response.Reason)
				assert.Equal(t, i18n.Tr(req.Context(), tc.key), response.Error)
				integrations.AssertNotCalled(t, "Connect", mock.Anything, mock.Anything)
			})
		}
	}
}
