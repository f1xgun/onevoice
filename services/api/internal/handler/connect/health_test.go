package connect

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/f1xgun/onevoice/services/api/internal/service/connhealth"
)

// telegramMemberMock serves getMe + getChatMember with a configurable
// membership verdict; getChat is answered generically so EvaluateTelegramHealth
// has a stable base.
func telegramMemberMock(t *testing.T, status string, canPost bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/getMe"):
			_, _ = fmt.Fprint(w, `{"ok":true,"result":{"id":777}}`)
		case strings.Contains(r.URL.Path, "/getChatMember"):
			_, _ = fmt.Fprintf(w, `{"ok":true,"result":{"status":%q,"can_post_messages":%t}}`, status, canPost)
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
