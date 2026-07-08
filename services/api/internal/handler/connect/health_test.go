package connect

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/f1xgun/onevoice/services/api/internal/service/connhealth"
)

// telegramMemberMock serves getMe + getChatMember with a configurable
// membership verdict; getChat reports a channel so EvaluateTelegramHealth has a
// stable base (post-rights are enforced for channels).
func telegramMemberMock(t *testing.T, status string, canPost bool) *httptest.Server {
	t.Helper()
	return telegramMemberMockTyped(t, status, canPost, "channel")
}

// telegramMemberMockTyped is telegramMemberMock with the getChat chat type made
// explicit, so tests can exercise the channel-vs-supergroup post-rights nuance.
func telegramMemberMockTyped(t *testing.T, status string, canPost bool, chatType string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/getMe"):
			_, _ = fmt.Fprint(w, `{"ok":true,"result":{"id":777}}`)
		case strings.Contains(r.URL.Path, "/getChatMember"):
			_, _ = fmt.Fprintf(w, `{"ok":true,"result":{"status":%q,"can_post_messages":%t}}`, status, canPost)
		default:
			_, _ = fmt.Fprintf(w, `{"ok":true,"result":{"id":-100,"title":"Ch","type":%q}}`, chatType)
		}
	}))
}

// telegramRateLimitedMemberMock serves a healthy getMe/getChat but a 429
// "Too Many Requests" envelope on getChatMember, modeling Telegram's global
// rate limit tripping mid-probe on the shared system bot token.
func telegramRateLimitedMemberMock(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/getMe"):
			_, _ = fmt.Fprint(w, `{"ok":true,"result":{"id":777}}`)
		case strings.Contains(r.URL.Path, "/getChatMember"):
			_, _ = fmt.Fprint(w, `{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 5"}`)
		default:
			_, _ = fmt.Fprint(w, `{"ok":true,"result":{"id":-100,"title":"Ch","type":"channel"}}`)
		}
	}))
}

func telegramHealthHandler(t *testing.T, srv *httptest.Server) *ConnectHandler {
	t.Helper()
	cfg := ConnectConfig{TelegramBotToken: "bot_token", telegramAPIBaseURL: srv.URL}
	return NewConnectHandler(new(MockConnectIntegrationService), new(MockBusinessService), nil, cfg, srv.Client())
}

// TestEvaluateTelegramHealth_CachesBotID proves the bot id is resolved once and
// reused across probes, so a fleet-wide health pass does not re-call getMe per
// channel against the single shared bot token.
func TestEvaluateTelegramHealth_CachesBotID(t *testing.T) {
	var getMeHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/getMe"):
			atomic.AddInt32(&getMeHits, 1)
			_, _ = fmt.Fprint(w, `{"ok":true,"result":{"id":777}}`)
		case strings.Contains(r.URL.Path, "/getChatMember"):
			_, _ = fmt.Fprint(w, `{"ok":true,"result":{"status":"administrator","can_post_messages":true}}`)
		default:
			_, _ = fmt.Fprint(w, `{"ok":true,"result":{"id":-100,"title":"Ch","type":"channel"}}`)
		}
	}))
	defer srv.Close()
	h := telegramHealthHandler(t, srv)

	const probes = 5
	for i := 0; i < probes; i++ {
		if res := h.EvaluateTelegramHealth(context.Background(), "bot_token", "@ch"); res.Status != connhealth.StatusActive {
			t.Fatalf("probe %d: expected active, got %+v", i, res)
		}
	}
	if got := atomic.LoadInt32(&getMeHits); got != 1 {
		t.Fatalf("expected getMe called once across %d probes (bot id cached), got %d", probes, got)
	}
}

func TestTelegramGetChatMember_MemberNotAdmin_Broken(t *testing.T) {
	srv := telegramMemberMock(t, "member", false)
	defer srv.Close()
	h := telegramHealthHandler(t, srv)

	res := h.EvaluateTelegramHealth(context.Background(), "bot_token", "@ch")
	if res.Status != connhealth.StatusBroken || res.ReasonCode != connhealth.ReasonTelegramNotAdmin {
		t.Fatalf("expected broken/tg_not_admin, got %+v", res)
	}
}

func TestTelegramGetChatMember_AdminNoPostRights_Broken(t *testing.T) {
	srv := telegramMemberMock(t, "administrator", false)
	defer srv.Close()
	h := telegramHealthHandler(t, srv)

	res := h.EvaluateTelegramHealth(context.Background(), "bot_token", "@ch")
	if res.Status != connhealth.StatusBroken || res.ReasonCode != connhealth.ReasonTelegramNoPostRight {
		t.Fatalf("expected broken/tg_no_post_rights, got %+v", res)
	}
}

func TestTelegramGetChatMember_CreatorActive(t *testing.T) {
	srv := telegramMemberMock(t, "creator", false)
	defer srv.Close()
	h := telegramHealthHandler(t, srv)

	res := h.EvaluateTelegramHealth(context.Background(), "bot_token", "@ch")
	if res.Status != connhealth.StatusActive {
		t.Fatalf("expected creator active, got %+v", res)
	}
}

func TestTelegramGetChatMember_AdminWithPostActive(t *testing.T) {
	srv := telegramMemberMock(t, "administrator", true)
	defer srv.Close()
	h := telegramHealthHandler(t, srv)

	res := h.EvaluateTelegramHealth(context.Background(), "bot_token", "@ch")
	if res.Status != connhealth.StatusActive {
		t.Fatalf("expected admin+post active, got %+v", res)
	}
}

func TestTelegramGetChatMember_Unreachable_Unknown(t *testing.T) {
	srv := telegramMemberMock(t, "creator", true)
	srv.Close() // dial failures => transport error => fail-soft unknown

	h := telegramHealthHandler(t, srv)
	res := h.EvaluateTelegramHealth(context.Background(), "bot_token", "@ch")
	if res.Status != connhealth.StatusUnknown {
		t.Fatalf("expected fail-soft unknown on transport failure, got %+v", res)
	}
	if res.Status == connhealth.StatusBroken {
		t.Fatalf("an unreachable probe must NOT be reported as broken")
	}
}

func TestTelegramGetChatMember_RateLimited_Unknown(t *testing.T) {
	srv := telegramRateLimitedMemberMock(t)
	defer srv.Close()
	h := telegramHealthHandler(t, srv)

	res := h.EvaluateTelegramHealth(context.Background(), "bot_token", "@ch")
	if res.Status != connhealth.StatusUnknown {
		t.Fatalf("a 429 getChatMember must fail soft to unknown, got %+v", res)
	}
	if res.Status == connhealth.StatusBroken {
		t.Fatalf("a rate-limited probe must NOT be reported as broken/not-admin")
	}
}

func TestTelegramAdmin_SupergroupNoPostFlag_Active(t *testing.T) {
	srv := telegramMemberMockTyped(t, "administrator", false, "supergroup")
	defer srv.Close()
	h := telegramHealthHandler(t, srv)

	res := h.EvaluateTelegramHealth(context.Background(), "bot_token", "-100123")
	if res.Status != connhealth.StatusActive {
		t.Fatalf("a supergroup administrator (can_post absent) can still post — expected active, got %+v", res)
	}
}

func TestTelegramAdmin_ChannelNoPostFlag_Broken(t *testing.T) {
	srv := telegramMemberMockTyped(t, "administrator", false, "channel")
	defer srv.Close()
	h := telegramHealthHandler(t, srv)

	res := h.EvaluateTelegramHealth(context.Background(), "bot_token", "@ch")
	if res.Status != connhealth.StatusBroken || res.ReasonCode != connhealth.ReasonTelegramNoPostRight {
		t.Fatalf("a channel administrator without post rights must be broken/no_post_rights, got %+v", res)
	}
}

func TestEvaluateVKHealth_CommunityProbeRateLimited_Unknown(t *testing.T) {
	vkServer := newVKAPIMock(t, vkMockOpts{
		getByIDErrorCode: 6, // Too many requests — transient, must fail soft
		getByIDErrorMsg:  "Too many requests per second",
		scopes:           []string{"wall"},
	})
	defer vkServer.Close()
	cfg := ConnectConfig{vkAPIBaseURL: vkServer.URL}
	h := NewConnectHandler(new(MockConnectIntegrationService), new(MockBusinessService), nil, cfg, vkServer.Client())

	res := h.EvaluateVKHealth(context.Background(), "tok")
	if res.Status != connhealth.StatusUnknown {
		t.Fatalf("a rate-limited groups.getById must fail soft to unknown, got %+v", res)
	}
	if res.Status == connhealth.StatusBroken {
		t.Fatalf("a rate-limited community probe must NOT be reported as broken")
	}
}

func TestEvaluateVKHealth_CommunityProbeCaptcha_Unknown(t *testing.T) {
	vkServer := newVKAPIMock(t, vkMockOpts{
		getByIDErrorCode: 14, // Captcha needed — anti-bot, must fail soft
		getByIDErrorMsg:  "Captcha needed",
		scopes:           []string{"wall"},
	})
	defer vkServer.Close()
	cfg := ConnectConfig{vkAPIBaseURL: vkServer.URL}
	h := NewConnectHandler(new(MockConnectIntegrationService), new(MockBusinessService), nil, cfg, vkServer.Client())

	res := h.EvaluateVKHealth(context.Background(), "tok")
	if res.Status != connhealth.StatusUnknown {
		t.Fatalf("a captcha-challenged groups.getById must fail soft to unknown, got %+v", res)
	}
}

func TestEvaluateVKHealth_CommunityProbeInvalidToken_Broken(t *testing.T) {
	vkServer := newVKAPIMock(t, vkMockOpts{
		getByIDErrorCode: 5, // Invalid token — conclusive auth failure
		getByIDErrorMsg:  "User authorization failed: invalid access_token.",
		scopes:           []string{"wall"},
	})
	defer vkServer.Close()
	cfg := ConnectConfig{vkAPIBaseURL: vkServer.URL}
	h := NewConnectHandler(new(MockConnectIntegrationService), new(MockBusinessService), nil, cfg, vkServer.Client())

	res := h.EvaluateVKHealth(context.Background(), "tok")
	if res.Status != connhealth.StatusBroken || res.ReasonCode != connhealth.ReasonVKTokenInvalid {
		t.Fatalf("a conclusive invalid-token envelope must be broken/vk_token_invalid, got %+v", res)
	}
}

func TestEvaluateVKHealth_WallScopeConclusiveMissing_Broken(t *testing.T) {
	vkServer := newVKAPIMock(t, vkMockOpts{
		communityID:   1,
		communityName: "Comm",
		scopes:        []string{"manage", "messages"}, // no wall
	})
	defer vkServer.Close()
	cfg := ConnectConfig{vkAPIBaseURL: vkServer.URL}
	h := NewConnectHandler(new(MockConnectIntegrationService), new(MockBusinessService), nil, cfg, vkServer.Client())

	res := h.EvaluateVKHealth(context.Background(), "tok")
	if res.Status != connhealth.StatusBroken || res.ReasonCode != connhealth.ReasonVKWallScopeMissing {
		t.Fatalf("expected broken/vk_wall_scope_missing, got %+v", res)
	}
}

func TestEvaluateVKHealth_TokenPermissionsRateLimited_Unknown(t *testing.T) {
	vkServer := newVKAPIMock(t, vkMockOpts{
		communityID:         1,
		communityName:       "Comm",
		tokenPermsErrorCode: 6, // Too many requests
		tokenPermsErrorMsg:  "Too many requests per second",
	})
	defer vkServer.Close()
	cfg := ConnectConfig{vkAPIBaseURL: vkServer.URL}
	h := NewConnectHandler(new(MockConnectIntegrationService), new(MockBusinessService), nil, cfg, vkServer.Client())

	res := h.EvaluateVKHealth(context.Background(), "tok")
	if res.Status != connhealth.StatusUnknown {
		t.Fatalf("expected fail-soft unknown on rate-limit, got %+v", res)
	}
}

func TestEvaluateVKHealth_WallPresent_Active(t *testing.T) {
	vkServer := newVKAPIMock(t, vkMockOpts{
		communityID:   1,
		communityName: "Comm",
		scopes:        []string{"wall", "manage"},
	})
	defer vkServer.Close()
	cfg := ConnectConfig{vkAPIBaseURL: vkServer.URL}
	h := NewConnectHandler(new(MockConnectIntegrationService), new(MockBusinessService), nil, cfg, vkServer.Client())

	res := h.EvaluateVKHealth(context.Background(), "tok")
	if res.Status != connhealth.StatusActive {
		t.Fatalf("expected active when wall scope present, got %+v", res)
	}
}
