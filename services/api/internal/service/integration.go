package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/crypto"
	"github.com/f1xgun/onevoice/pkg/domain"
)

// ConnectParams holds parameters for connecting a new platform integration
type ConnectParams struct {
	BusinessID   uuid.UUID
	Platform     string
	ExternalID   string
	AccessToken  string
	RefreshToken string
	Metadata     map[string]interface{}
	ExpiresAt    *time.Time
}

// TokenResponse holds decrypted token data for a platform integration
type TokenResponse struct {
	IntegrationID uuid.UUID              `json:"integration_id"`
	Platform      string                 `json:"platform"`
	ExternalID    string                 `json:"external_id"`
	AccessToken   string                 `json:"access_token"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	ExpiresAt     *time.Time             `json:"expires_at,omitempty"`
}

// IntegrationService defines the interface for platform integration management
type IntegrationService interface {
	ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]domain.Integration, error)
	GetByBusinessAndPlatform(ctx context.Context, businessID uuid.UUID, platform string) (*domain.Integration, error)
	Delete(ctx context.Context, integrationID uuid.UUID) error

	Connect(ctx context.Context, params ConnectParams) (*domain.Integration, error)
	GetDecryptedToken(ctx context.Context, businessID uuid.UUID, platform, externalID string) (*TokenResponse, error)
	ListByBusinessAndPlatform(ctx context.Context, businessID uuid.UUID, platform string) ([]domain.Integration, error)
}

type integrationService struct {
	repo domain.IntegrationRepository
	enc  *crypto.Encryptor
}

// Compile-time check that integrationService implements IntegrationService
var _ IntegrationService = (*integrationService)(nil)

// NewIntegrationService creates a new integration service instance
func NewIntegrationService(repo domain.IntegrationRepository, enc *crypto.Encryptor) IntegrationService {
	return &integrationService{
		repo: repo,
		enc:  enc,
	}
}

// ListByBusinessID retrieves all integrations for a business
func (s *integrationService) ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]domain.Integration, error) {
	// Check context
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Validate business ID
	if businessID == uuid.Nil {
		return nil, fmt.Errorf("business id is required")
	}

	integrations, err := s.repo.ListByBusinessID(ctx, businessID)
	if err != nil {
		return nil, fmt.Errorf("list integrations: %w", err)
	}

	return integrations, nil
}

// GetByBusinessAndPlatform retrieves a specific integration by business and platform
func (s *integrationService) GetByBusinessAndPlatform(ctx context.Context, businessID uuid.UUID, platform string) (*domain.Integration, error) {
	// Check context
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Validate business ID
	if businessID == uuid.Nil {
		return nil, fmt.Errorf("business id is required")
	}

	// Validate platform
	if platform == "" {
		return nil, fmt.Errorf("platform is required")
	}

	integration, err := s.repo.GetByBusinessAndPlatform(ctx, businessID, platform)
	if err != nil {
		if errors.Is(err, domain.ErrIntegrationNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("get integration: %w", err)
	}

	return integration, nil
}

// Delete removes an integration
func (s *integrationService) Delete(ctx context.Context, integrationID uuid.UUID) error {
	// Check context
	if err := ctx.Err(); err != nil {
		return err
	}

	// Validate integration ID
	if integrationID == uuid.Nil {
		return fmt.Errorf("integration id is required")
	}

	err := s.repo.Delete(ctx, integrationID)
	if err != nil {
		if errors.Is(err, domain.ErrIntegrationNotFound) {
			return err
		}
		return fmt.Errorf("delete integration: %w", err)
	}

	return nil
}

// Connect creates a new platform integration, encrypting tokens before storage
func (s *integrationService) Connect(ctx context.Context, params ConnectParams) (*domain.Integration, error) {
	if params.BusinessID == uuid.Nil {
		return nil, fmt.Errorf("business id is required")
	}
	if params.Platform == "" {
		return nil, fmt.Errorf("platform is required")
	}

	var encAccess, encRefresh []byte
	var err error
	if params.AccessToken != "" {
		encAccess, err = s.enc.Encrypt([]byte(params.AccessToken))
		if err != nil {
			return nil, fmt.Errorf("encrypt access token: %w", err)
		}
	}
	if params.RefreshToken != "" {
		encRefresh, err = s.enc.Encrypt([]byte(params.RefreshToken))
		if err != nil {
			return nil, fmt.Errorf("encrypt refresh token: %w", err)
		}
	}

	integration := &domain.Integration{
		BusinessID:            params.BusinessID,
		Platform:              params.Platform,
		Status:                "active",
		ExternalID:            params.ExternalID,
		EncryptedAccessToken:  encAccess,
		EncryptedRefreshToken: encRefresh,
		Metadata:              params.Metadata,
		TokenExpiresAt:        params.ExpiresAt,
	}

	if err := s.repo.Create(ctx, integration); err != nil {
		return nil, err
	}

	return integration, nil
}

// GetDecryptedToken retrieves and decrypts the access token for a specific integration.
// When externalID is empty, the first active integration for the platform is used.
func (s *integrationService) GetDecryptedToken(ctx context.Context, businessID uuid.UUID, platform, externalID string) (*TokenResponse, error) {
	var integration *domain.Integration
	var err error

	if externalID != "" {
		integration, err = s.repo.GetByBusinessPlatformExternal(ctx, businessID, platform, externalID)
		if err != nil && !errors.Is(err, domain.ErrIntegrationNotFound) {
			return nil, err
		}
	}

	// Fall back to the first active integration if not found by externalID
	if integration == nil {
		integrations, listErr := s.repo.ListByBusinessAndPlatform(ctx, businessID, platform)
		if listErr != nil {
			return nil, listErr
		}
		for i := range integrations {
			if integrations[i].Status == "active" {
				integration = &integrations[i]
				break
			}
		}
		if integration == nil {
			return nil, domain.ErrIntegrationNotFound
		}
	}

	// Check expiration
	if integration.TokenExpiresAt != nil && integration.TokenExpiresAt.Before(time.Now()) {
		// No automatic refresh for now — return expired error
		return nil, domain.ErrTokenExpired
	}

	var accessToken string
	if len(integration.EncryptedAccessToken) > 0 {
		decrypted, err := s.enc.Decrypt(integration.EncryptedAccessToken)
		if err != nil {
			return nil, fmt.Errorf("decrypt access token: %w", err)
		}
		accessToken = string(decrypted)
	}

	return &TokenResponse{
		IntegrationID: integration.ID,
		Platform:      integration.Platform,
		ExternalID:    integration.ExternalID,
		AccessToken:   accessToken,
		Metadata:      integration.Metadata,
		ExpiresAt:     integration.TokenExpiresAt,
	}, nil
}

// ListByBusinessAndPlatform retrieves all integrations for a business filtered by platform
func (s *integrationService) ListByBusinessAndPlatform(ctx context.Context, businessID uuid.UUID, platform string) ([]domain.Integration, error) {
	if businessID == uuid.Nil {
		return nil, fmt.Errorf("business id is required")
	}
	return s.repo.ListByBusinessAndPlatform(ctx, businessID, platform)
}
