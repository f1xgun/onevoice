package agentbase

import (
	"context"
	"fmt"
)

// FetchToken resolves a TokenInfo for (businessID, platform, externalID) via
// the supplied TokenResolver and wraps any error as a typed agentbase
// token-fetch error so the orchestrator surfaces the proper CodedError to
// the frontend. reason records the caller's purpose (typically the A2A tool
// name) for the server-side decrypt audit row.
func FetchToken(ctx context.Context, resolver TokenResolver, businessID, platform, externalID, reason string) (TokenInfo, error) {
	info, err := resolver.GetToken(ctx, businessID, platform, externalID, reason)
	if err != nil {
		return TokenInfo{}, WrapTokenFetchError(fmt.Errorf("fetch token: %w", err))
	}
	return info, nil
}
