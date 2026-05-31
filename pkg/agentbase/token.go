package agentbase

import (
	"context"
	"fmt"
)

// FetchToken resolves a TokenInfo for (businessID, platform, externalID) via
// the supplied TokenResolver and wraps any error as a typed agentbase
// token-fetch error so the orchestrator surfaces the proper CodedError to
// the frontend.
func FetchToken(ctx context.Context, resolver TokenResolver, businessID, platform, externalID string) (TokenInfo, error) {
	info, err := resolver.GetToken(ctx, businessID, platform, externalID)
	if err != nil {
		return TokenInfo{}, WrapTokenFetchError(fmt.Errorf("fetch token: %w", err))
	}
	return info, nil
}
