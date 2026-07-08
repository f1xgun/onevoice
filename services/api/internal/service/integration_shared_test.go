package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/tools"
)

// TestConnect_DelegatedEmptyToken_StoresNoCredential verifies the additive
// property at the service layer: a delegated Connect (empty AccessToken) creates
// an active integration whose EncryptedAccessToken is empty, with the permalink
// as external_id and the delegated metadata preserved. GetDecryptedToken then
// returns an empty access token + the permalink, exactly what the agent's
// delegated branch keys on.
func TestConnect_DelegatedEmptyToken_StoresNoCredential(t *testing.T) {
	ctx := context.Background()
	businessID := uuid.New()

	var created *domain.Integration
	repo := &mockIntegrationRepository{
		createFunc: func(_ context.Context, integration *domain.Integration) error {
			created = integration
			return nil
		},
	}
	svc := NewIntegrationService(repo, testKMSEnvelope(t), nil, nil, audit.Nop())

	integration, err := svc.Connect(ctx, ConnectParams{
		BusinessID:  businessID,
		Platform:    a2a.AgentYandexBusiness,
		ExternalID:  "114697172504",
		AccessToken: "",
		Metadata: map[string]interface{}{
			"connect_mode":    tools.ConnectModeDelegated,
			"access_verified": false,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, integration)
	assert.Empty(t, created.EncryptedAccessToken, "delegated integration must store NO encrypted access token")
	assert.Equal(t, "114697172504", created.ExternalID)
	assert.Equal(t, domain.IntegrationStatusActive, created.Status)
	assert.Equal(t, tools.ConnectModeDelegated, created.Metadata["connect_mode"])

	repo.getByBusinessPlatformExternalFunc = func(_ context.Context, _ uuid.UUID, _, _ string) (*domain.Integration, error) {
		return created, nil
	}
	tok, err := svc.GetDecryptedToken(ctx, businessID, a2a.AgentYandexBusiness, "114697172504", "test")
	require.NoError(t, err)
	assert.Empty(t, tok.AccessToken, "delegated integration decrypts to an empty access token")
	assert.Equal(t, "114697172504", tok.ExternalID, "permalink flows back as external_id")
}

// TestSetSharedSession_StoresEncryptedSingleton verifies the shared-session
// bootstrap: it stores the credential encrypted under the sentinel business +
// reserved external_id, and it decrypts back through GetDecryptedToken.
func TestSetSharedSession_StoresEncryptedSingleton(t *testing.T) {
	ctx := context.Background()
	sharedBusinessID := uuid.New()
	const cookieJSON = `[{"name":"Session_id","value":"shared-secret"}]`

	var created *domain.Integration
	repo := &mockIntegrationRepository{
		createFunc: func(_ context.Context, integration *domain.Integration) error {
			created = integration
			return nil
		},
	}
	svc := NewIntegrationService(repo, testKMSEnvelope(t), nil, nil, audit.Nop())

	integration, err := svc.SetSharedSession(ctx, SharedSessionParams{
		SharedBusinessID: sharedBusinessID,
		Platform:         a2a.AgentYandexBusiness,
		Credential:       cookieJSON,
	})
	require.NoError(t, err)
	require.NotNil(t, integration)
	assert.Equal(t, tools.YandexSharedRepExternalID, created.ExternalID, "must store under the reserved sentinel external_id")
	assert.Equal(t, sharedBusinessID, created.BusinessID)
	assert.NotEmpty(t, created.EncryptedAccessToken, "shared session must be encrypted at rest")

	repo.getByBusinessPlatformExternalFunc = func(_ context.Context, _ uuid.UUID, _, _ string) (*domain.Integration, error) {
		return created, nil
	}
	tok, err := svc.GetDecryptedToken(ctx, sharedBusinessID, a2a.AgentYandexBusiness, tools.YandexSharedRepExternalID, "shared_fetch")
	require.NoError(t, err)
	assert.Equal(t, cookieJSON, tok.AccessToken, "shared session must decrypt back to the original cookie JSON")
}

// TestSetSharedSession_RetiresPrevious verifies a rotate soft-deletes the prior
// sentinel row before creating the fresh one.
func TestSetSharedSession_RetiresPrevious(t *testing.T) {
	ctx := context.Background()
	sharedBusinessID := uuid.New()
	priorID := uuid.New()

	var softDeleted uuid.UUID
	repo := &mockIntegrationRepository{
		getByBusinessPlatformExternalFunc: func(_ context.Context, _ uuid.UUID, _, ext string) (*domain.Integration, error) {
			if ext == tools.YandexSharedRepExternalID {
				return &domain.Integration{ID: priorID, BusinessID: sharedBusinessID, ExternalID: ext}, nil
			}
			return nil, domain.ErrIntegrationNotFound
		},
		softDeleteFunc: func(_ context.Context, id uuid.UUID) error {
			softDeleted = id
			return nil
		},
	}
	svc := NewIntegrationService(repo, testKMSEnvelope(t), nil, nil, audit.Nop())

	_, err := svc.SetSharedSession(ctx, SharedSessionParams{
		SharedBusinessID: sharedBusinessID,
		Platform:         a2a.AgentYandexBusiness,
		Credential:       `[{"name":"Session_id","value":"new"}]`,
	})
	require.NoError(t, err)
	assert.Equal(t, priorID, softDeleted, "rotate must soft-delete the prior shared session row")
}

// TestSetSharedSession_RejectsEmptyCredential guards the fail-closed contract:
// an empty credential is rejected rather than storing an unauthenticated
// singleton.
func TestSetSharedSession_RejectsEmptyCredential(t *testing.T) {
	svc := NewIntegrationService(&mockIntegrationRepository{}, testKMSEnvelope(t), nil, nil, audit.Nop())
	_, err := svc.SetSharedSession(context.Background(), SharedSessionParams{
		SharedBusinessID: uuid.New(),
		Platform:         a2a.AgentYandexBusiness,
		Credential:       "",
	})
	require.Error(t, err)
}
