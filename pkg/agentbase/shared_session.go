package agentbase

import (
	"context"
	"errors"
	"fmt"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/tools"
)

// ErrSharedSessionNotConfigured is returned by a SharedSessionResolver whose
// shared-business sentinel is unset. It signals the delegated plane is inert
// (nothing provisioned) so callers fail closed with a clear "not configured"
// error rather than acting on a partially-wired shared account.
var ErrSharedSessionNotConfigured = errors.New("shared representative session not configured")

// SharedSessionResolver resolves the single KMS-wrapped representative session
// cookie JSON shared across every delegated Yandex org. It is the SESSION PLANE
// counterpart to the per-business TokenResolver: exactly one credential, keyed
// by the config-pinned shared business sentinel + platform + reserved
// external_id, decrypted through the same envelope machinery every other
// integration token flows through.
type SharedSessionResolver interface {
	// GetSharedSession returns the decrypted shared session credential (cookie
	// JSON or OAuth token) for the given platform. reason records the caller's
	// purpose for the server-side decrypt audit row. It returns
	// ErrSharedSessionNotConfigured when the sentinel business is unset, and the
	// underlying tokenclient sentinel errors (ErrIntegrationNotFound /
	// ErrTokenExpired / ErrTransient) otherwise, so callers can branch via
	// errors.Is exactly as they do for per-business tokens.
	GetSharedSession(ctx context.Context, platform, reason string) (string, error)
}

// sharedSessionResolver is the default SharedSessionResolver. It delegates to a
// TokenResolver, pinning the (businessID, external_id) coordinates of the shared
// singleton row so the caller supplies only the platform + reason.
type sharedSessionResolver struct {
	resolver         TokenResolver
	sharedBusinessID string
	externalID       string
}

// NewSharedSessionResolver wraps a TokenResolver into a SharedSessionResolver
// pinned to the shared-representative singleton row. sharedBusinessID is the
// config sentinel (YANDEX_SHARED_BUSINESS_ID); an empty value makes every
// GetSharedSession fail with ErrSharedSessionNotConfigured, which is how the
// delegated plane stays inert until an operator provisions it. resolver must be
// non-nil — a nil resolver is a wiring bug surfaced at construction.
func NewSharedSessionResolver(resolver TokenResolver, sharedBusinessID string) SharedSessionResolver {
	if resolver == nil {
		panic("agentbase.NewSharedSessionResolver: resolver cannot be nil")
	}
	return &sharedSessionResolver{
		resolver:         resolver,
		sharedBusinessID: sharedBusinessID,
		externalID:       tools.YandexSharedRepExternalID,
	}
}

// GetSharedSession fetches the pinned shared-session row through the wrapped
// TokenResolver and returns its access token (the cookie JSON). When the shared
// business sentinel is unset it fails closed with ErrSharedSessionNotConfigured
// before any lookup. Errors from the resolver are wrapped through
// WrapTokenFetchError so an expired/absent shared session surfaces the same
// coded, non-retryable error the per-business path uses.
func (s *sharedSessionResolver) GetSharedSession(ctx context.Context, platform, reason string) (string, error) {
	if s.sharedBusinessID == "" {
		return "", ErrSharedSessionNotConfigured
	}
	info, err := s.resolver.GetToken(ctx, s.sharedBusinessID, platform, s.externalID, reason)
	if err != nil {
		return "", WrapTokenFetchError(fmt.Errorf("fetch shared session: %w", err))
	}
	if info.AccessToken == "" {
		return "", a2a.NewCodedError("integration_token_invalid", a2a.NewNonRetryableError(ErrSharedSessionNotConfigured))
	}
	return info.AccessToken, nil
}

// Compile-time check: sharedSessionResolver satisfies SharedSessionResolver.
var _ SharedSessionResolver = (*sharedSessionResolver)(nil)
