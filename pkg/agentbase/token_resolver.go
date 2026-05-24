package agentbase

import (
	"context"
	"errors"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/tokenclient"
)

// TokenInfo is the canonical credentials shape resolved by the API's
// /internal/v1/tokens endpoint. UserToken is populated only when the agent's
// platform supports a separate user-scoped token (VK currently — empty string
// for other platforms; downstream consumers should treat empty as absent).
//
// The struct mirrors the per-agent TokenInfo definitions in
// services/agent-{telegram,vk,yandex-business,google-business}/internal/agent/
// handler.go. After the agent migration, those per-agent
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
//
// Errors are sentinel-chained (tokenclient.ErrIntegrationNotFound /
// ErrTokenExpired / ErrTransient); callers should branch via errors.Is.
// WrapTokenFetchError applies the canonical retryability policy and is
// what every agent handler in this repo uses — prefer it over rolling a
// new classification.
type TokenResolver interface {
	GetToken(ctx context.Context, businessID, platform, externalID string) (TokenInfo, error)
}

// WrapTokenFetchError classifies a token-fetch error for the agent retry
// policy. The four agent handlers
// (services/agent-{telegram,vk,yandex-business,google-business}/internal/agent/handler.go)
// all need the same branch:
//
//   - tokenclient.ErrTransient → return as-is so the error stays
//     retryable (network blip, 5xx upstream — retrying may succeed).
//   - anything else (ErrIntegrationNotFound, ErrTokenExpired, request-shape
//     bugs) → wrap in *a2a.NonRetryableError so withRetry + the LLM-side
//     tool_result both see "permanent failure, do not retry."
//
// Callers pass the already-contextualized error (e.g.
// fmt.Errorf("fetch token: %w", err)) — the sentinel chain survives the
// wrap so errors.Is keeps working.
func WrapTokenFetchError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, tokenclient.ErrTransient) {
		return err
	}
	return a2a.NewNonRetryableError(err)
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
// "no speculative surface" rule.
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
