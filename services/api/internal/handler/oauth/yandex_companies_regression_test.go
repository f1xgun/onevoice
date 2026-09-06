package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/crypto"
	"github.com/f1xgun/onevoice/pkg/i18n"
)

type companiesPublisher func(context.Context, string, a2a.ToolRequest, time.Duration) (*a2a.ToolResponse, error)

func (f companiesPublisher) RequestTool(ctx context.Context, subject string, req a2a.ToolRequest, timeout time.Duration) (*a2a.ToolResponse, error) {
	return f(ctx, subject, req, timeout)
}

func TestListYandexCompanies_ClassifiedErrorsAndDeadline(t *testing.T) {
	for _, tt := range []struct{ code, key string }{
		{"integration_token_invalid", "oauth.yandex.session_expired"},
		{"transient", "oauth.yandex.list_orgs_failed"},
		{"rate_limit_exceeded", "oauth.yandex.list_orgs_failed"},
	} {
		t.Run(tt.code, func(t *testing.T) {
			h := NewOAuthHandler(new(MockOAuthStateService), new(MockOAuthIntegrationService), new(MockBusinessService), OAuthConfig{}, nil, nil)
			enc, err := crypto.NewEncryptor([]byte(strings.Repeat("x", 32)))
			require.NoError(t, err)
			h.WithPayloadEncryptor(enc)
			h.WithAgentTaskPublisher(companiesPublisher(func(ctx context.Context, subject string, req a2a.ToolRequest, timeout time.Duration) (*a2a.ToolResponse, error) {
				require.Equal(t, a2a.Subject(a2a.AgentYandexBusiness), subject)
				require.GreaterOrEqual(t, timeout, 70*time.Second)
				require.NotNil(t, req.Deadline)
				require.Greater(t, time.Until(*req.Deadline), 100*time.Second)
				require.Less(t, time.Until(*req.Deadline), timeout)
				require.Contains(t, req.Args, "cookies_enc")
				return &a2a.ToolResponse{Error: "private upstream detail", Code: tt.code}, nil
			}))
			body, err := json.Marshal(map[string]string{"cookies": validYandexSession})
			require.NoError(t, err)
			req := httptest.NewRequest(http.MethodPost, "/companies", strings.NewReader(string(body)))
			req = req.WithContext(oauthBizCtx(uuid.New(), uuid.New(), authz.PermIntegrationsConnect))
			rr := httptest.NewRecorder()
			h.ListYandexCompanies(rr, req)
			require.Equal(t, http.StatusBadGateway, rr.Code)
			var result map[string]string
			require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &result))
			require.Equal(t, tt.code, result["code"])
			require.Equal(t, i18n.Tr(req.Context(), tt.key), result["error"])
			require.NotContains(t, rr.Body.String(), "private upstream detail")
		})
	}
}

func TestRequestYandexCompanies_PreservesEarlierCallerDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	want, _ := ctx.Deadline()
	h := &OAuthHandler{taskPublisher: companiesPublisher(func(gotCtx context.Context, _ string, req a2a.ToolRequest, _ time.Duration) (*a2a.ToolResponse, error) {
		require.Equal(t, ctx, gotCtx)
		require.NotNil(t, req.Deadline)
		require.True(t, want.Equal(*req.Deadline))
		return &a2a.ToolResponse{Success: true}, nil
	})}
	_, err := h.requestYandexCompanies(ctx, a2a.ToolRequest{})
	require.NoError(t, err)
}
