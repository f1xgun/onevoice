package oauth

import (
	"context"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// MockOAuthStateService mocks OAuthStateService.
type MockOAuthStateService struct {
	mock.Mock
}

func (m *MockOAuthStateService) GenerateState(ctx context.Context, data service.OAuthStateData) (state, nonce string, err error) {
	args := m.Called(ctx, data)
	return args.String(0), args.String(1), args.Error(2)
}

func (m *MockOAuthStateService) ValidateState(ctx context.Context, state string) (*service.OAuthStateData, error) {
	args := m.Called(ctx, state)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.OAuthStateData), args.Error(1)
}

// MockOAuthIntegrationService mocks OAuthIntegrationService.
type MockOAuthIntegrationService struct {
	mock.Mock
}

func (m *MockOAuthIntegrationService) Connect(ctx context.Context, params service.ConnectParams) (*domain.Integration, error) {
	args := m.Called(ctx, params)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Integration), args.Error(1)
}

func (m *MockOAuthIntegrationService) ListByBusinessAndPlatform(ctx context.Context, businessID uuid.UUID, platform string) ([]domain.Integration, error) {
	args := m.Called(ctx, businessID, platform)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.Integration), args.Error(1)
}

func (m *MockOAuthIntegrationService) UpdateMetadata(ctx context.Context, integrationID uuid.UUID, metadata map[string]interface{}) error {
	args := m.Called(ctx, integrationID, metadata)
	return args.Error(0)
}

func (m *MockOAuthIntegrationService) UpdateExternalID(ctx context.Context, integrationID uuid.UUID, externalID string) error {
	args := m.Called(ctx, integrationID, externalID)
	return args.Error(0)
}

func (m *MockOAuthIntegrationService) GetDecryptedToken(ctx context.Context, businessID uuid.UUID, platform, externalID, reason string) (*service.TokenResponse, error) {
	args := m.Called(ctx, businessID, platform, externalID, reason)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.TokenResponse), args.Error(1)
}

// MockBusinessService is a mock implementation of the BusinessService
// interface (handler/oauth.BusinessService — narrower than
// handler.BusinessService).
type MockBusinessService struct {
	mock.Mock
}

// oauthBizCtx seeds an authz.BusinessContext with the given perms for handlers
// under PermIntegrationsConnect.
func oauthBizCtx(businessID, userID uuid.UUID, perms ...authz.Permission) context.Context {
	return authz.WithBusinessContext(context.Background(), authz.BusinessContext{
		BusinessID:  businessID,
		UserID:      userID,
		RoleID:      uuid.New(),
		Permissions: perms,
	})
}
