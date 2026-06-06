package wire

import (
	"context"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// IntegrationSyncAdapterImpl bridges service.IntegrationService to the
// narrow integrationProvider interface that platform.NewSyncer expects.
// The platform package needs only List + GetDecryptedToken; IntegrationService
// exposes a richer surface, so we shim down to what the syncer requires.
//
// Suffixed with Impl (rather than naming this IntegrationSyncAdapter) so
// the constructor below — IntegrationSyncAdapter — can stay verb-less,
// matching how the original cmd/main.go spelled it.
type IntegrationSyncAdapterImpl struct {
	svc service.IntegrationService
}

// IntegrationSyncAdapter constructs the bridge that platform.NewSyncer
// expects. The returned *IntegrationSyncAdapterImpl satisfies
// platform.integrationProvider via structural typing.
func IntegrationSyncAdapter(svc service.IntegrationService) *IntegrationSyncAdapterImpl {
	return &IntegrationSyncAdapterImpl{svc: svc}
}

func (a *IntegrationSyncAdapterImpl) ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]domain.Integration, error) {
	return a.svc.ListByBusinessID(ctx, businessID)
}

func (a *IntegrationSyncAdapterImpl) GetDecryptedToken(ctx context.Context, businessID uuid.UUID, plt, externalID, reason string) (string, error) {
	resp, err := a.svc.GetDecryptedToken(ctx, businessID, plt, externalID, reason)
	if err != nil {
		return "", err
	}
	return resp.AccessToken, nil
}
