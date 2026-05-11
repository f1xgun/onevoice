package connect

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// MockConnectIntegrationService mocks ConnectIntegrationService.
type MockConnectIntegrationService struct {
	mock.Mock
}

func (m *MockConnectIntegrationService) Connect(ctx context.Context, params service.ConnectParams) (*domain.Integration, error) {
	args := m.Called(ctx, params)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Integration), args.Error(1)
}

func (m *MockConnectIntegrationService) ListByBusinessAndPlatform(ctx context.Context, businessID uuid.UUID, platform string) ([]domain.Integration, error) {
	args := m.Called(ctx, businessID, platform)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.Integration), args.Error(1)
}

func (m *MockConnectIntegrationService) UpdateMetadata(ctx context.Context, integrationID uuid.UUID, metadata map[string]interface{}) error {
	args := m.Called(ctx, integrationID, metadata)
	return args.Error(0)
}

func (m *MockConnectIntegrationService) GetDecryptedToken(ctx context.Context, businessID uuid.UUID, platform, externalID string) (*service.TokenResponse, error) {
	args := m.Called(ctx, businessID, platform, externalID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.TokenResponse), args.Error(1)
}

// MockBusinessService is a mock implementation of BusinessService.
type MockBusinessService struct {
	mock.Mock
}

// connectBizCtx seeds an authz.BusinessContext for connect handlers (Phase 2
// v2.0 RBAC). Mirrors oauthBizCtx in handler/oauth/mocks_test.go.
func connectBizCtx(businessID, userID uuid.UUID, perms ...authz.Permission) context.Context {
	return authz.WithBusinessContext(context.Background(), authz.BusinessContext{
		BusinessID:  businessID,
		UserID:      userID,
		RoleID:      uuid.New(),
		Permissions: perms,
	})
}

// buildTelegramHash builds a valid Telegram HMAC-SHA256 hash for the given fields.
func buildTelegramHash(token string, fields map[string]interface{}) string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, fields[k]))
	}
	checkString := strings.Join(parts, "\n")
	secretKey := sha256.Sum256([]byte(token))
	mac := hmac.New(sha256.New, secretKey[:])
	mac.Write([]byte(checkString))
	return hex.EncodeToString(mac.Sum(nil))
}
