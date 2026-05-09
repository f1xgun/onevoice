package wire

import (
	"context"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// integrationSyncAdapter bridges service.IntegrationService to
// platform.integrationProvider. The platform package needs a narrow view
// (List + GetDecryptedToken) — IntegrationService exposes a richer surface,
// so we shim down to what the platform syncer expects.
type integrationSyncAdapter struct {
	svc service.IntegrationService
}

// IntegrationSyncAdapter constructs the bridge that platform.NewSyncer
// expects. The returned value satisfies platform.integrationProvider.
func IntegrationSyncAdapter(svc service.IntegrationService) *integrationSyncAdapter {
	return &integrationSyncAdapter{svc: svc}
}

func (a *integrationSyncAdapter) ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]domain.Integration, error) {
	return a.svc.ListByBusinessID(ctx, businessID)
}

func (a *integrationSyncAdapter) GetDecryptedToken(ctx context.Context, businessID uuid.UUID, plt, externalID string) (string, error) {
	resp, err := a.svc.GetDecryptedToken(ctx, businessID, plt, externalID)
	if err != nil {
		return "", err
	}
	return resp.AccessToken, nil
}
