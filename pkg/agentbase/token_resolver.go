package agentbase

import (
	"context"

	"github.com/f1xgun/onevoice/pkg/tokenclient"
)

// TokenInfo is the canonical credentials shape resolved by the API's
// /internal/v1/tokens endpoint. UserToken is populated only when the agent's
// platform supports a separate user-scoped token (VK currently — empty string
// for other platforms; downstream consumers should treat empty as absent).
//
// The struct mirrors the per-agent TokenInfo definitions in
// services/agent-{telegram,vk,yandex-business,google-business}/internal/agent/
// handler.go — phase 19-RESEARCH §5a ("tokenAdapter — duplicated 4×")
// documents the audit. After plan 19-07 migrates the agents, those per-agent
// structs become aliases (or are deleted in favor of this one).
type TokenInfo struct {
	AccessToken string
	UserToken   string
	ExternalID  string
}

// TokenResolver fetches a TokenInfo for (businessID, platform, externalID).
// When externalID is empty the underlying *tokenclient.Client falls back to
// the first active integration for the platform — same semantics as the API
// server's GetDecryptedToken (services/api/internal/service/integration.go).
// Errors from the underlying HTTP client are returned wrapped per the
// tokenclient package conventions; callers must NOT string-match them.
type TokenResolver interface {
	GetToken(ctx context.Context, businessID, platform, externalID string) (TokenInfo, error)
}

// tokenResolverImpl is the default TokenResolver that delegates to a
// *tokenclient.Client. It exists only to satisfy the interface — there is no
// per-platform variant; the four hand-rolled tokenAdapter struct definitions
// in agent cmd/main.go files (telegram:95-108, vk:93-108, yandex:89-102,
// google:88-101) are byte-identical apart from whether they propagate
// UserToken. This impl always propagates UserToken; non-VK platforms get an
// empty string, which their consumers ignore.
type tokenResolverImpl struct {
	client *tokenclient.Client
}

// NewTokenResolver wraps a *tokenclient.Client. The caller retains ownership
// of the client — multiple TokenResolver values may share one client for cache
// reuse. Panics if client is nil; agentbase requires a real HTTP client at
// construction time, not at call time, to surface wiring bugs at boot rather
// than at the first tool dispatch.
func NewTokenResolver(c *tokenclient.Client) TokenResolver {
	if c == nil {
		panic("agentbase.NewTokenResolver: client cannot be nil")
	}
	return &tokenResolverImpl{client: c}
}

// GetToken delegates to the wrapped *tokenclient.Client and remaps the
// response into a TokenInfo. The mapping is the same one repeated across the
// four agent tokenAdapter implementations: AccessToken / UserToken /
// ExternalID. Any other fields on tokenclient.TokenResponse (Metadata,
// ExpiresAt, UserTokenExpires, IntegrationID) are intentionally NOT exposed
// because no agent currently consumes them — adding them would violate the
// "no speculative surface" rule (phase 19 SPEC risk #5).
func (r *tokenResolverImpl) GetToken(ctx context.Context, businessID, platform, externalID string) (TokenInfo, error) {
	resp, err := r.client.GetToken(ctx, businessID, platform, externalID)
	if err != nil {
		return TokenInfo{}, err
	}
	return TokenInfo{
		AccessToken: resp.AccessToken,
		UserToken:   resp.UserToken,
		ExternalID:  resp.ExternalID,
	}, nil
}

// Compile-time check: tokenResolverImpl satisfies TokenResolver.
var _ TokenResolver = (*tokenResolverImpl)(nil)
