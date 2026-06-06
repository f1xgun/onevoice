package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/crypto"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/pkg/metrics"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
)

// TokenRefresher abstracts the HTTP call to refresh an expired OAuth token.
// See docs/services/integration.md.
type TokenRefresher interface {
	// RefreshToken exchanges a refresh token for a new access token (newRefreshToken empty if not rotated).
	RefreshToken(ctx context.Context, refreshToken string) (accessToken string, newRefreshToken string, expiresIn int64, err error)
}

// ConnectParams holds parameters for connecting a new platform integration.
// See docs/services/integration.md.
type ConnectParams struct {
	BusinessID       uuid.UUID
	ActorID          uuid.UUID
	Platform         string
	ExternalID       string
	AccessToken      string
	RefreshToken     string
	UserToken        string     // VK user token for read operations (optional)
	UserTokenExpires *time.Time // VK user token expiration (optional)
	Metadata         map[string]interface{}
	ExpiresAt        *time.Time
}

// TokenResponse holds decrypted token data for a platform integration.
type TokenResponse struct {
	IntegrationID    uuid.UUID              `json:"integration_id"`
	Platform         string                 `json:"platform"`
	ExternalID       string                 `json:"external_id"`
	AccessToken      string                 `json:"access_token"`
	UserToken        string                 `json:"user_token,omitempty"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
	ExpiresAt        *time.Time             `json:"expires_at,omitempty"`
	UserTokenExpires *time.Time             `json:"user_token_expires_at,omitempty"`
}

// NATSPublisher is the narrow publish surface the integration service needs to
// fan out a revoke on delete. *nats.Conn satisfies it directly.
type NATSPublisher interface {
	Publish(subject string, data []byte) error
}

// IntegrationService defines the interface for platform integration management.
// See docs/services/integration.md.
type IntegrationService interface {
	ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]domain.Integration, error)
	GetByBusinessAndPlatform(ctx context.Context, businessID uuid.UUID, platform string) (*domain.Integration, error)
	Delete(ctx context.Context, integrationID uuid.UUID, actorID uuid.UUID) error

	Connect(ctx context.Context, params ConnectParams) (*domain.Integration, error)
	GetDecryptedToken(ctx context.Context, businessID uuid.UUID, platform, externalID, reason string) (*TokenResponse, error)
	ListByBusinessAndPlatform(ctx context.Context, businessID uuid.UUID, platform string) ([]domain.Integration, error)
	UpdateMetadata(ctx context.Context, integrationID uuid.UUID, metadata map[string]interface{}) error
	UpdateExternalID(ctx context.Context, integrationID uuid.UUID, externalID string) error
}

type integrationService struct {
	repo      domain.IntegrationRepository
	enc       *crypto.Encryptor
	refreshMu sync.Map       // map[uuid.UUID]*sync.Mutex — per-integration refresh lock
	refresher TokenRefresher // nil for platforms that don't need refresh
	audit     audit.Logger
	nats      NATSPublisher // nil when NATS is unreachable — revoke publish is skipped (fail-open)
}

// Compile-time check that integrationService implements IntegrationService
var _ IntegrationService = (*integrationService)(nil)

// NewIntegrationService constructs the service; refresher/auditLogger may be nil (auto-defaulted to Nop).
// See docs/services/integration.md.
func NewIntegrationService(repo domain.IntegrationRepository, enc *crypto.Encryptor, refresher TokenRefresher, auditLogger audit.Logger) IntegrationService {
	if auditLogger == nil {
		auditLogger = audit.Nop()
	}
	return &integrationService{
		repo:      repo,
		enc:       enc,
		refresher: refresher,
		audit:     auditLogger,
	}
}

// WithNATSPublisher attaches the publisher used to fan out a revoke when an
// integration is deleted, returning svc for chaining. When the publisher is
// unset (or nil), or svc is not the concrete service, Delete skips the publish
// and relies on the cache TTL backstop. Constructed separately from the
// constructor so existing call sites that do not need fan-out are unaffected.
func WithNATSPublisher(svc IntegrationService, p NATSPublisher) IntegrationService {
	if s, ok := svc.(*integrationService); ok {
		s.nats = p
	}
	return svc
}

// getRefreshMutex returns a per-integration mutex for serializing refresh calls.
func (s *integrationService) getRefreshMutex(id uuid.UUID) *sync.Mutex {
	val, _ := s.refreshMu.LoadOrStore(id, &sync.Mutex{})
	return val.(*sync.Mutex)
}

// ListByBusinessID retrieves all integrations for a business
func (s *integrationService) ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]domain.Integration, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

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
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if businessID == uuid.Nil {
		return nil, fmt.Errorf("business id is required")
	}

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

// UpdateMetadata replaces only the metadata jsonb; token fields are preserved.
// See docs/services/integration.md.
func (s *integrationService) UpdateMetadata(ctx context.Context, integrationID uuid.UUID, metadata map[string]interface{}) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if integrationID == uuid.Nil {
		return fmt.Errorf("integration id is required")
	}

	integration, err := s.repo.GetByID(ctx, integrationID)
	if err != nil {
		if errors.Is(err, domain.ErrIntegrationNotFound) {
			return err
		}
		return fmt.Errorf("get integration: %w", err)
	}

	if metadata == nil {
		metadata = map[string]interface{}{}
	}
	integration.Metadata = metadata

	if err := s.repo.Update(ctx, integration); err != nil {
		return fmt.Errorf("update integration: %w", err)
	}
	return nil
}

// UpdateExternalID heals the external_id post-connect (e.g. Yandex Sprav permalink resolved later).
// See docs/services/integration.md.
func (s *integrationService) UpdateExternalID(ctx context.Context, integrationID uuid.UUID, externalID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if integrationID == uuid.Nil {
		return fmt.Errorf("integration id is required")
	}
	if externalID == "" {
		return fmt.Errorf("external id is required")
	}

	integration, err := s.repo.GetByID(ctx, integrationID)
	if err != nil {
		if errors.Is(err, domain.ErrIntegrationNotFound) {
			return err
		}
		return fmt.Errorf("get integration: %w", err)
	}
	integration.ExternalID = externalID

	if err := s.repo.Update(ctx, integration); err != nil {
		return fmt.Errorf("update integration: %w", err)
	}
	return nil
}

// Delete soft-deletes an integration, emits an integration.deleted audit row,
// and fans out a revoke on integrations.revoked.{platform}.{businessID} so live
// agent caches invalidate the token. The publish is fail-open: a publish error
// is logged + metered but never blocks the deletion (the cache TTL is the
// backstop). actorID identifies the user who performed the deletion.
func (s *integrationService) Delete(ctx context.Context, integrationID uuid.UUID, actorID uuid.UUID) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if integrationID == uuid.Nil {
		return fmt.Errorf("integration id is required")
	}

	integ, err := s.repo.GetByID(ctx, integrationID)
	if err != nil {
		if errors.Is(err, domain.ErrIntegrationNotFound) {
			return err
		}
		return fmt.Errorf("get integration: %w", err)
	}

	if err := s.repo.SoftDelete(ctx, integrationID); err != nil {
		if errors.Is(err, domain.ErrIntegrationNotFound) {
			return err
		}
		return fmt.Errorf("soft-delete: %w", err)
	}

	audit.LogIntegrationDeleted(ctx, s.audit, integ.BusinessID, actorID, integrationID, integ.Platform, integ.ExternalID)

	if s.nats != nil {
		subject := fmt.Sprintf("integrations.revoked.%s.%s", integ.Platform, integ.BusinessID.String())
		if err := s.nats.Publish(subject, []byte("{}")); err != nil {
			slog.WarnContext(ctx, "revoke publish failed; cache TTL backstop will handle",
				"subject", subject, "error", err)
			metrics.IncIntegrationsRevokePublishFailed()
		}
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

	var encAccess, encRefresh, encUser []byte
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
	if params.UserToken != "" {
		encUser, err = s.enc.Encrypt([]byte(params.UserToken))
		if err != nil {
			return nil, fmt.Errorf("encrypt user token: %w", err)
		}
	}

	metadata := params.Metadata
	if metadata == nil {
		metadata = map[string]interface{}{}
	}

	integration := &domain.Integration{
		BusinessID:            params.BusinessID,
		Platform:              params.Platform,
		Status:                domain.IntegrationStatusActive,
		ExternalID:            params.ExternalID,
		EncryptedAccessToken:  encAccess,
		EncryptedRefreshToken: encRefresh,
		EncryptedUserToken:    encUser,
		Metadata:              metadata,
		TokenExpiresAt:        params.ExpiresAt,
		UserTokenExpiresAt:    params.UserTokenExpires,
	}

	if err := s.repo.Create(ctx, integration); err != nil {
		return nil, err
	}

	audit.LogIntegrationConnected(ctx, s.audit, params.BusinessID, params.ActorID, integration.ID, params.Platform, params.ExternalID, "", "", "")

	return integration, nil
}

// GetDecryptedToken returns decrypted tokens, refreshing on expiry; empty externalID falls back to first-active.
// A synchronous fail-closed audit row is emitted before the token is returned;
// if the audit INSERT fails the token is never released. reason records the
// caller's purpose for the forensic trail.
// See docs/services/integration.md.
func (s *integrationService) GetDecryptedToken(ctx context.Context, businessID uuid.UUID, platform, externalID, reason string) (*TokenResponse, error) {
	var integration *domain.Integration
	var err error

	if externalID != "" {
		integration, err = s.repo.GetByBusinessPlatformExternal(ctx, businessID, platform, externalID)
		if err != nil && !errors.Is(err, domain.ErrIntegrationNotFound) {
			return nil, err
		}
	}

	if integration == nil {
		integrations, listErr := s.repo.ListByBusinessAndPlatform(ctx, businessID, platform)
		if listErr != nil {
			return nil, listErr
		}
		for i := range integrations {
			if integrations[i].Status == domain.IntegrationStatusActive {
				integration = &integrations[i]
				break
			}
		}
		if integration == nil {
			return nil, domain.ErrIntegrationNotFound
		}
	}

	if integration.TokenExpiresAt != nil && integration.TokenExpiresAt.Before(time.Now()) {
		if len(integration.EncryptedRefreshToken) == 0 || s.refresher == nil {
			return nil, domain.ErrTokenExpired
		}

		mu := s.getRefreshMutex(integration.ID)
		mu.Lock()
		defer mu.Unlock()

		integration, err = s.repo.GetByID(ctx, integration.ID)
		if err != nil {
			return nil, fmt.Errorf("re-read integration after lock: %w", err)
		}

		if integration.TokenExpiresAt != nil && integration.TokenExpiresAt.Before(time.Now()) {
			refreshToken, err := s.enc.Decrypt(integration.EncryptedRefreshToken)
			if err != nil {
				return nil, fmt.Errorf("decrypt refresh token: %w", err)
			}

			newAccess, newRefresh, expiresIn, err := s.refresher.RefreshToken(ctx, string(refreshToken))
			if err != nil {
				slog.ErrorContext(ctx, "token refresh failed",
					"integration_id", integration.ID,
					"platform", integration.Platform,
					"error", err,
				)
				return nil, domain.ErrTokenExpired
			}

			encAccess, err := s.enc.Encrypt([]byte(newAccess))
			if err != nil {
				return nil, fmt.Errorf("encrypt refreshed access token: %w", err)
			}
			integration.EncryptedAccessToken = encAccess

			if newRefresh != "" {
				encRefresh, err := s.enc.Encrypt([]byte(newRefresh))
				if err != nil {
					return nil, fmt.Errorf("encrypt rotated refresh token: %w", err)
				}
				integration.EncryptedRefreshToken = encRefresh
			}

			expiresAt := time.Now().Add(time.Duration(expiresIn) * time.Second)
			integration.TokenExpiresAt = &expiresAt

			if err := s.repo.Update(ctx, integration); err != nil {
				return nil, fmt.Errorf("persist refreshed tokens: %w", err)
			}

			audit.LogIntegrationTokenRotated(ctx, s.audit, integration.BusinessID, integration.ID, integration.Platform)

			slog.InfoContext(ctx, "token refreshed successfully",
				"integration_id", integration.ID,
				"platform", integration.Platform,
				"new_expiry", expiresAt.Format(time.RFC3339),
			)
		}
	}

	var accessToken string
	if len(integration.EncryptedAccessToken) > 0 {
		decrypted, err := s.enc.Decrypt(integration.EncryptedAccessToken)
		if err != nil {
			return nil, fmt.Errorf("decrypt access token: %w", err)
		}
		accessToken = string(decrypted)
	}

	var userToken string
	if len(integration.EncryptedUserToken) > 0 {
		decrypted, err := s.enc.Decrypt(integration.EncryptedUserToken)
		if err != nil {
			return nil, fmt.Errorf("decrypt user token: %w", err)
		}
		if integration.UserTokenExpiresAt == nil || integration.UserTokenExpiresAt.After(time.Now()) {
			userToken = string(decrypted)
		}
	}

	caller := middleware.ServiceIdentityFromContext(ctx)
	if caller == "" {
		caller = "api.internal"
	}
	correlationID := logger.CorrelationIDFromContext(ctx)
	if err := audit.LogTokenDecryptedSync(ctx, s.audit, businessID, integration.ID, platform, caller, correlationID, reason); err != nil {
		return nil, fmt.Errorf("audit token_decrypted: %w", err)
	}
	metrics.IncIntegrationTokenDecrypted(platform, caller)

	return &TokenResponse{
		IntegrationID:    integration.ID,
		Platform:         integration.Platform,
		ExternalID:       integration.ExternalID,
		AccessToken:      accessToken,
		UserToken:        userToken,
		Metadata:         integration.Metadata,
		ExpiresAt:        integration.TokenExpiresAt,
		UserTokenExpires: integration.UserTokenExpiresAt,
	}, nil
}

// ListByBusinessAndPlatform retrieves all integrations for a business filtered by platform
func (s *integrationService) ListByBusinessAndPlatform(ctx context.Context, businessID uuid.UUID, platform string) ([]domain.Integration, error) {
	if businessID == uuid.Nil {
		return nil, fmt.Errorf("business id is required")
	}
	return s.repo.ListByBusinessAndPlatform(ctx, businessID, platform)
}
