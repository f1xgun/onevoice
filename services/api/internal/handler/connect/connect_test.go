package connect

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

func TestConnectTelegram_ForensicMetadata(t *testing.T) {
	tgServer := newTelegramAPIMockWithLinkedGroup(t, "My Channel", 0, true)
	defer tgServer.Close()

	userID := uuid.New()
	businessID := uuid.New()

	var captured service.ConnectParams
	mockIntegration := new(MockConnectIntegrationService)
	mockBusiness := new(MockBusinessService)
	mockIntegration.On("Connect", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			captured = args.Get(1).(service.ConnectParams)
		}).
		Return(&domain.Integration{ID: uuid.New(), Platform: "telegram"}, nil)

	cfg := ConnectConfig{TelegramBotToken: "bot_token_123", telegramAPIBaseURL: tgServer.URL}
	h := NewConnectHandler(mockIntegration, mockBusiness, cfg, tgServer.Client())

	req := httptest.NewRequest(http.MethodPost, "/oauth/telegram/connect",
		strings.NewReader(`{"channel_id":"@mychannel"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "ForensicUA/2.0")
	req.RemoteAddr = "203.0.113.7:54321"
	req = req.WithContext(connectBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()

	h.ConnectTelegram(rr, req)

	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
	require.Equal(t, "203.0.113.7", captured.ActorIP)
	require.Equal(t, "ForensicUA/2.0", captured.UserAgent)
	require.Equal(t, "bot_token", captured.ParsedFormat)
}

func TestConnectVKCommunity_ForensicMetadata(t *testing.T) {
	vkServer := newVKAPIMock(t, vkMockOpts{
		communityID:         236912172,
		communityName:       "OneVoice",
		communityScreenName: "club236912172",
		scopes:              []string{"wall", "manage", "messages"},
	})
	defer vkServer.Close()

	userID := uuid.New()
	businessID := uuid.New()

	var captured service.ConnectParams
	mockIntegration := new(MockConnectIntegrationService)
	mockBusiness := new(MockBusinessService)
	mockIntegration.On("Connect", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			captured = args.Get(1).(service.ConnectParams)
		}).
		Return(&domain.Integration{ID: uuid.New(), Platform: "vk", ExternalID: "236912172"}, nil)

	cfg := ConnectConfig{vkAPIBaseURL: vkServer.URL}
	h := NewConnectHandler(mockIntegration, mockBusiness, cfg, vkServer.Client())

	req := httptest.NewRequest(http.MethodPost, "/integrations/vk/connect",
		strings.NewReader(`{"access_token": "vk1.a.PASTED_COMMUNITY_TOKEN"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "ForensicUA/3.0")
	req.RemoteAddr = "198.51.100.9:40000"
	req = req.WithContext(connectBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()

	h.ConnectVK(rr, req)

	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
	require.Equal(t, "198.51.100.9", captured.ActorIP)
	require.Equal(t, "ForensicUA/3.0", captured.UserAgent)
	require.Equal(t, "access_token", captured.ParsedFormat)
}
