package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/crypto"
	"github.com/f1xgun/onevoice/pkg/crypto/kmsfake"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/metrics"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
)

// Mock IntegrationRepository
type mockIntegrationRepository struct {
	createFunc                        func(ctx context.Context, integration *domain.Integration) error
	getByIDFunc                       func(ctx context.Context, id uuid.UUID) (*domain.Integration, error)
	getByBusinessAndPlatformFunc      func(ctx context.Context, businessID uuid.UUID, platform string) (*domain.Integration, error)
	listByBusinessIDFunc              func(ctx context.Context, businessID uuid.UUID) ([]domain.Integration, error)
	listByBusinessAndPlatformFunc     func(ctx context.Context, businessID uuid.UUID, platform string) ([]domain.Integration, error)
	getByBusinessPlatformExternalFunc func(ctx context.Context, businessID uuid.UUID, platform, externalID string) (*domain.Integration, error)
	updateFunc                        func(ctx context.Context, integration *domain.Integration) error
	deleteFunc                        func(ctx context.Context, id uuid.UUID) error
	softDeleteFunc                    func(ctx context.Context, id uuid.UUID) error
	deleteOlderThanFunc               func(ctx context.Context, cutoff time.Time) (int64, error)
	markTokenExpiredFunc              func(ctx context.Context, businessID uuid.UUID, platform, externalID string) (int64, error)
}

func (m *mockIntegrationRepository) Create(ctx context.Context, integration *domain.Integration) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, integration)
	}
	return nil
}

func (m *mockIntegrationRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Integration, error) {
	if m.getByIDFunc != nil {
		return m.getByIDFunc(ctx, id)
	}
	return nil, domain.ErrIntegrationNotFound
}

func (m *mockIntegrationRepository) GetByBusinessAndPlatform(ctx context.Context, businessID uuid.UUID, platform string) (*domain.Integration, error) {
	if m.getByBusinessAndPlatformFunc != nil {
		return m.getByBusinessAndPlatformFunc(ctx, businessID, platform)
	}
	return nil, domain.ErrIntegrationNotFound
}

func (m *mockIntegrationRepository) ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]domain.Integration, error) {
	if m.listByBusinessIDFunc != nil {
		return m.listByBusinessIDFunc(ctx, businessID)
	}
	return []domain.Integration{}, nil
}

func (m *mockIntegrationRepository) ListByBusinessAndPlatform(ctx context.Context, businessID uuid.UUID, platform string) ([]domain.Integration, error) {
	if m.listByBusinessAndPlatformFunc != nil {
		return m.listByBusinessAndPlatformFunc(ctx, businessID, platform)
	}
	return []domain.Integration{}, nil
}

func (m *mockIntegrationRepository) GetByBusinessPlatformExternal(ctx context.Context, businessID uuid.UUID, platform, externalID string) (*domain.Integration, error) {
	if m.getByBusinessPlatformExternalFunc != nil {
		return m.getByBusinessPlatformExternalFunc(ctx, businessID, platform, externalID)
	}
	return nil, domain.ErrIntegrationNotFound
}

func (m *mockIntegrationRepository) Update(ctx context.Context, integration *domain.Integration) error {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, integration)
	}
	return nil
}

func (m *mockIntegrationRepository) ListAllActiveByPlatforms(ctx context.Context, platforms []string) ([]domain.Integration, error) {
	return []domain.Integration{}, nil
}

func (m *mockIntegrationRepository) Delete(ctx context.Context, id uuid.UUID) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, id)
	}
	return nil
}

func (m *mockIntegrationRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	if m.softDeleteFunc != nil {
		return m.softDeleteFunc(ctx, id)
	}
	return nil
}

func (m *mockIntegrationRepository) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	if m.deleteOlderThanFunc != nil {
		return m.deleteOlderThanFunc(ctx, cutoff)
	}
	return 0, nil
}

func (m *mockIntegrationRepository) MarkTokenExpired(ctx context.Context, businessID uuid.UUID, platform, externalID string) (int64, error) {
	if m.markTokenExpiredFunc != nil {
		return m.markTokenExpiredFunc(ctx, businessID, platform, externalID)
	}
	return 0, nil
}

func (m *mockIntegrationRepository) CountIntegrationsWithDifferentFingerprint(_ context.Context, _ string) (int, error) {
	return 0, nil
}

func (m *mockIntegrationRepository) SelectForRekey(_ context.Context, _ pgx.Tx, _ int16, _ int) ([]domain.Integration, error) {
	return nil, nil
}

func (m *mockIntegrationRepository) UpdateEnvelopeFieldsTx(_ context.Context, _ pgx.Tx, _ domain.Integration) error {
	return nil
}

func (m *mockIntegrationRepository) CountRekeyRemaining(_ context.Context, _ int16) (int, error) {
	return 0, nil
}

// testEncryptor creates a test encryptor with a 32-byte key.
func testEncryptor(t *testing.T) *crypto.Encryptor {
	t.Helper()
	testKey := []byte("12345678901234567890123456789012")
	enc, err := crypto.NewEncryptor(testKey)
	require.NoError(t, err)
	return enc
}

// testEnvelope wraps a given Encryptor in a legacy-only Envelope (no KMS).
func testEnvelope(t *testing.T, enc *crypto.Encryptor) *crypto.Envelope {
	t.Helper()
	return crypto.NewEnvelope(nil, enc, "", nil)
}

// testKMSEnvelope builds an Envelope backed by the in-memory fake KMS, the
// production envelope-encryption path where each row carries a wrapped DEK.
func testKMSEnvelope(t *testing.T) *crypto.Envelope {
	t.Helper()
	return crypto.NewEnvelope(kmsfake.New(), nil, "test-kms-key-id", map[string]int16{"1": 1})
}

func TestIntegrationService_ListByBusinessID(t *testing.T) {
	ctx := context.Background()

	t.Run("success with integrations", func(t *testing.T) {
		businessID := uuid.New()
		integrations := []domain.Integration{
			{
				ID:                   uuid.New(),
				BusinessID:           businessID,
				Platform:             "google",
				Status:               "active",
				EncryptedAccessToken: []byte("encrypted_token_1"),
				ExternalID:           "ext_google_123",
				Metadata:             map[string]interface{}{"location_id": "loc_123"},
				CreatedAt:            time.Now(),
				UpdatedAt:            time.Now(),
			},
			{
				ID:                   uuid.New(),
				BusinessID:           businessID,
				Platform:             "vk",
				Status:               "pending",
				EncryptedAccessToken: []byte("encrypted_token_2"),
				ExternalID:           "ext_vk_456",
				Metadata:             map[string]interface{}{"group_id": "123456"},
				CreatedAt:            time.Now(),
				UpdatedAt:            time.Now(),
			},
		}

		repo := &mockIntegrationRepository{
			listByBusinessIDFunc: func(ctx context.Context, id uuid.UUID) ([]domain.Integration, error) {
				if id == businessID {
					return integrations, nil
				}
				return []domain.Integration{}, nil
			},
		}

		svc := NewIntegrationService(repo, testEnvelope(t, testEncryptor(t)), nil, nil, audit.Nop())
		result, err := svc.ListByBusinessID(ctx, businessID)

		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Len(t, result, 2)
		assert.Equal(t, "google", result[0].Platform)
		assert.Equal(t, "vk", result[1].Platform)
	})

	t.Run("success with empty list", func(t *testing.T) {
		businessID := uuid.New()
		repo := &mockIntegrationRepository{
			listByBusinessIDFunc: func(ctx context.Context, id uuid.UUID) ([]domain.Integration, error) {
				return []domain.Integration{}, nil
			},
		}

		svc := NewIntegrationService(repo, testEnvelope(t, testEncryptor(t)), nil, nil, audit.Nop())
		result, err := svc.ListByBusinessID(ctx, businessID)

		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Len(t, result, 0)
	})

	t.Run("error - nil business id", func(t *testing.T) {
		repo := &mockIntegrationRepository{}
		svc := NewIntegrationService(repo, testEnvelope(t, testEncryptor(t)), nil, nil, audit.Nop())

		result, err := svc.ListByBusinessID(ctx, uuid.Nil)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "business id is required")
	})

	t.Run("error - canceled context", func(t *testing.T) {
		cancelledCtx, cancel := context.WithCancel(context.Background())
		cancel()

		repo := &mockIntegrationRepository{}
		svc := NewIntegrationService(repo, testEnvelope(t, testEncryptor(t)), nil, nil, audit.Nop())

		result, err := svc.ListByBusinessID(cancelledCtx, uuid.New())

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("error - repository error", func(t *testing.T) {
		repoErr := errors.New("database connection failed")
		repo := &mockIntegrationRepository{
			listByBusinessIDFunc: func(ctx context.Context, id uuid.UUID) ([]domain.Integration, error) {
				return nil, repoErr
			},
		}

		svc := NewIntegrationService(repo, testEnvelope(t, testEncryptor(t)), nil, nil, audit.Nop())
		result, err := svc.ListByBusinessID(ctx, uuid.New())

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "list integrations")
	})
}

func TestIntegrationService_GetByBusinessAndPlatform(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		businessID := uuid.New()
		platform := "google"
		existingIntegration := &domain.Integration{
			ID:                   uuid.New(),
			BusinessID:           businessID,
			Platform:             platform,
			Status:               "active",
			EncryptedAccessToken: []byte("encrypted_token"),
			ExternalID:           "ext_google_123",
			Metadata:             map[string]interface{}{"location_id": "loc_123"},
			CreatedAt:            time.Now(),
			UpdatedAt:            time.Now(),
		}

		repo := &mockIntegrationRepository{
			getByBusinessAndPlatformFunc: func(ctx context.Context, bid uuid.UUID, plat string) (*domain.Integration, error) {
				if bid == businessID && plat == platform {
					return existingIntegration, nil
				}
				return nil, domain.ErrIntegrationNotFound
			},
		}

		svc := NewIntegrationService(repo, testEnvelope(t, testEncryptor(t)), nil, nil, audit.Nop())
		result, err := svc.GetByBusinessAndPlatform(ctx, businessID, platform)

		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, existingIntegration.ID, result.ID)
		assert.Equal(t, existingIntegration.BusinessID, result.BusinessID)
		assert.Equal(t, existingIntegration.Platform, result.Platform)
		assert.Equal(t, existingIntegration.Status, result.Status)
	})

	t.Run("integration not found", func(t *testing.T) {
		repo := &mockIntegrationRepository{
			getByBusinessAndPlatformFunc: func(ctx context.Context, bid uuid.UUID, plat string) (*domain.Integration, error) {
				return nil, domain.ErrIntegrationNotFound
			},
		}

		svc := NewIntegrationService(repo, testEnvelope(t, testEncryptor(t)), nil, nil, audit.Nop())
		result, err := svc.GetByBusinessAndPlatform(ctx, uuid.New(), "google")

		assert.ErrorIs(t, err, domain.ErrIntegrationNotFound)
		assert.Nil(t, result)
	})

	t.Run("error - nil business id", func(t *testing.T) {
		repo := &mockIntegrationRepository{}
		svc := NewIntegrationService(repo, testEnvelope(t, testEncryptor(t)), nil, nil, audit.Nop())

		result, err := svc.GetByBusinessAndPlatform(ctx, uuid.Nil, "google")

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "business id is required")
	})

	t.Run("error - empty platform", func(t *testing.T) {
		repo := &mockIntegrationRepository{}
		svc := NewIntegrationService(repo, testEnvelope(t, testEncryptor(t)), nil, nil, audit.Nop())

		result, err := svc.GetByBusinessAndPlatform(ctx, uuid.New(), "")

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "platform is required")
	})

	t.Run("error - canceled context", func(t *testing.T) {
		cancelledCtx, cancel := context.WithCancel(context.Background())
		cancel()

		repo := &mockIntegrationRepository{}
		svc := NewIntegrationService(repo, testEnvelope(t, testEncryptor(t)), nil, nil, audit.Nop())

		result, err := svc.GetByBusinessAndPlatform(cancelledCtx, uuid.New(), "google")

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("error - repository error", func(t *testing.T) {
		repoErr := errors.New("database error")
		repo := &mockIntegrationRepository{
			getByBusinessAndPlatformFunc: func(ctx context.Context, bid uuid.UUID, plat string) (*domain.Integration, error) {
				return nil, repoErr
			},
		}

		svc := NewIntegrationService(repo, testEnvelope(t, testEncryptor(t)), nil, nil, audit.Nop())
		result, err := svc.GetByBusinessAndPlatform(ctx, uuid.New(), "google")

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "get integration")
	})
}

type stubNATSPublisher struct {
	published []publishedMsg
	err       error
}

type publishedMsg struct {
	subject string
	data    []byte
}

func (s *stubNATSPublisher) Publish(subject string, data []byte) error {
	s.published = append(s.published, publishedMsg{subject: subject, data: data})
	return s.err
}

func deleteTestIntegration(id, businessID uuid.UUID, platform, externalID string) *domain.Integration {
	return &domain.Integration{
		ID:         id,
		BusinessID: businessID,
		Platform:   platform,
		Status:     "active",
		ExternalID: externalID,
	}
}

func TestIntegrationService_Delete(t *testing.T) {
	ctx := context.Background()

	t.Run("success soft-deletes and does not hard-delete", func(t *testing.T) {
		integrationID := uuid.New()
		businessID := uuid.New()
		var softDeletedID uuid.UUID
		hardDeleted := false

		repo := &mockIntegrationRepository{
			getByIDFunc: func(_ context.Context, id uuid.UUID) (*domain.Integration, error) {
				return deleteTestIntegration(id, businessID, "telegram", "chan-1"), nil
			},
			softDeleteFunc: func(_ context.Context, id uuid.UUID) error {
				softDeletedID = id
				return nil
			},
			deleteFunc: func(_ context.Context, _ uuid.UUID) error {
				hardDeleted = true
				return nil
			},
		}

		svc := NewIntegrationService(repo, testEnvelope(t, testEncryptor(t)), nil, nil, audit.Nop())
		err := svc.Delete(ctx, integrationID, uuid.New())

		require.NoError(t, err)
		assert.Equal(t, integrationID, softDeletedID)
		assert.False(t, hardDeleted, "Delete must soft-delete, never hard-delete")
	})

	t.Run("GetByID failure aborts before soft-delete and publish", func(t *testing.T) {
		softDeleteCalled := false
		nats := &stubNATSPublisher{}
		repo := &mockIntegrationRepository{
			getByIDFunc: func(_ context.Context, _ uuid.UUID) (*domain.Integration, error) {
				return nil, domain.ErrIntegrationNotFound
			},
			softDeleteFunc: func(_ context.Context, _ uuid.UUID) error {
				softDeleteCalled = true
				return nil
			},
		}

		svc := NewIntegrationService(repo, testEnvelope(t, testEncryptor(t)), nil, nil, audit.Nop()).(*integrationService)
		svc.nats = nats
		err := svc.Delete(ctx, uuid.New(), uuid.New())

		assert.ErrorIs(t, err, domain.ErrIntegrationNotFound)
		assert.False(t, softDeleteCalled, "soft-delete must not run when GetByID fails")
		assert.Empty(t, nats.published, "publish must not run when GetByID fails")
	})

	t.Run("emits integration.deleted audit with actorID after soft-delete", func(t *testing.T) {
		integrationID := uuid.New()
		businessID := uuid.New()
		actorID := uuid.New()
		rec := &recordingSyncLogger{}

		repo := &mockIntegrationRepository{
			getByIDFunc: func(_ context.Context, id uuid.UUID) (*domain.Integration, error) {
				return deleteTestIntegration(id, businessID, "vk", "grp-9"), nil
			},
		}

		svc := NewIntegrationService(repo, testEnvelope(t, testEncryptor(t)), nil, nil, rec)
		require.NoError(t, svc.Delete(ctx, integrationID, actorID))

		require.Len(t, rec.asyncCalls, 1, "expected one integration.deleted audit entry")
		entry := rec.asyncCalls[0]
		assert.Equal(t, audit.ActionIntegrationDeleted, entry.Action)
		require.NotNil(t, entry.BusinessID)
		assert.Equal(t, businessID, *entry.BusinessID)
		require.NotNil(t, entry.UserID)
		assert.Equal(t, actorID, *entry.UserID)
	})

	t.Run("publishes revoke on integrations.revoked.<platform>.<businessID>", func(t *testing.T) {
		integrationID := uuid.New()
		businessID := uuid.New()
		nats := &stubNATSPublisher{}

		repo := &mockIntegrationRepository{
			getByIDFunc: func(_ context.Context, id uuid.UUID) (*domain.Integration, error) {
				return deleteTestIntegration(id, businessID, "telegram", "chan-1"), nil
			},
		}

		svc := NewIntegrationService(repo, testEnvelope(t, testEncryptor(t)), nil, nil, audit.Nop()).(*integrationService)
		svc.nats = nats
		require.NoError(t, svc.Delete(ctx, integrationID, uuid.New()))

		require.Len(t, nats.published, 1)
		assert.Equal(t, "integrations.revoked.telegram."+businessID.String(), nats.published[0].subject)
		assert.Equal(t, []byte("{}"), nats.published[0].data)
	})

	t.Run("publish failure is fail-open", func(t *testing.T) {
		integrationID := uuid.New()
		businessID := uuid.New()
		nats := &stubNATSPublisher{err: errors.New("nats down")}
		var softDeleted bool

		repo := &mockIntegrationRepository{
			getByIDFunc: func(_ context.Context, id uuid.UUID) (*domain.Integration, error) {
				return deleteTestIntegration(id, businessID, "telegram", "chan-1"), nil
			},
			softDeleteFunc: func(_ context.Context, _ uuid.UUID) error {
				softDeleted = true
				return nil
			},
		}

		svc := NewIntegrationService(repo, testEnvelope(t, testEncryptor(t)), nil, nil, audit.Nop()).(*integrationService)
		svc.nats = nats
		err := svc.Delete(ctx, integrationID, uuid.New())

		require.NoError(t, err, "publish failure must not abort deletion (fail-open)")
		assert.True(t, softDeleted, "soft-delete must have succeeded before the failed publish")
		require.Len(t, nats.published, 1, "publish must have been attempted")
	})

	t.Run("error - nil integration id", func(t *testing.T) {
		repo := &mockIntegrationRepository{}
		svc := NewIntegrationService(repo, testEnvelope(t, testEncryptor(t)), nil, nil, audit.Nop())

		err := svc.Delete(ctx, uuid.Nil, uuid.New())

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "integration id is required")
	})

	t.Run("error - canceled context", func(t *testing.T) {
		cancelledCtx, cancel := context.WithCancel(context.Background())
		cancel()

		repo := &mockIntegrationRepository{}
		svc := NewIntegrationService(repo, testEnvelope(t, testEncryptor(t)), nil, nil, audit.Nop())

		err := svc.Delete(cancelledCtx, uuid.New(), uuid.New())

		assert.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("error - soft-delete repository error aborts", func(t *testing.T) {
		businessID := uuid.New()
		nats := &stubNATSPublisher{}
		repo := &mockIntegrationRepository{
			getByIDFunc: func(_ context.Context, id uuid.UUID) (*domain.Integration, error) {
				return deleteTestIntegration(id, businessID, "telegram", "chan-1"), nil
			},
			softDeleteFunc: func(_ context.Context, _ uuid.UUID) error {
				return errors.New("database error")
			},
		}

		svc := NewIntegrationService(repo, testEnvelope(t, testEncryptor(t)), nil, nil, audit.Nop()).(*integrationService)
		svc.nats = nats
		err := svc.Delete(ctx, uuid.New(), uuid.New())

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "soft-delete")
		assert.Empty(t, nats.published, "publish must not run when soft-delete fails")
	})
}

// --- New tests for Connect, GetDecryptedToken, ListByBusinessAndPlatform ---

func TestConnect_Success(t *testing.T) {
	ctx := context.Background()
	enc := testEncryptor(t)

	businessID := uuid.New()
	plaintext := "my_secret_access_token"

	var capturedIntegration *domain.Integration
	repo := &mockIntegrationRepository{
		createFunc: func(ctx context.Context, integration *domain.Integration) error {
			capturedIntegration = integration
			return nil
		},
	}

	svc := NewIntegrationService(repo, testEnvelope(t, enc), nil, nil, audit.Nop())
	params := ConnectParams{
		BusinessID:  businessID,
		Platform:    "telegram",
		ExternalID:  "ext_telegram_123",
		AccessToken: plaintext,
	}
	result, err := svc.Connect(ctx, params)

	require.NoError(t, err)
	require.NotNil(t, result)

	require.NotNil(t, capturedIntegration)
	assert.NotEqual(t, []byte(plaintext), capturedIntegration.EncryptedAccessToken)
	assert.NotEmpty(t, capturedIntegration.EncryptedAccessToken)

	decrypted, err := enc.Decrypt(capturedIntegration.EncryptedAccessToken)
	require.NoError(t, err)
	assert.Equal(t, plaintext, string(decrypted))

	assert.Equal(t, "telegram", result.Platform)
	assert.Equal(t, "ext_telegram_123", result.ExternalID)
	assert.Equal(t, "active", result.Status)
}

func TestConnect_ForwardsActorIP(t *testing.T) {
	ctx := context.Background()
	enc := testEncryptor(t)
	businessID := uuid.New()

	repo := &mockIntegrationRepository{
		createFunc: func(_ context.Context, integration *domain.Integration) error {
			integration.ID = uuid.New()
			return nil
		},
	}

	rec := &recordingSyncLogger{}
	svc := NewIntegrationService(repo, testEnvelope(t, enc), nil, nil, rec)

	_, err := svc.Connect(ctx, ConnectParams{
		BusinessID:   businessID,
		Platform:     "yandex_business",
		ExternalID:   "ext_1",
		AccessToken:  "tok",
		ActorIP:      "1.2.3.4",
		UserAgent:    "UA/1.0",
		ParsedFormat: "json",
	})
	require.NoError(t, err)

	require.Len(t, rec.asyncCalls, 1)
	entry := rec.asyncCalls[0]
	assert.Equal(t, audit.ActionIntegrationConnected, entry.Action)

	var details audit.IntegrationConnectedDetails
	require.NoError(t, json.Unmarshal(entry.Details, &details))
	assert.Equal(t, "1.2.3.4", details.ActorIP)
	assert.Equal(t, "UA/1.0", details.UserAgent)
	assert.Equal(t, "json", details.ParsedFormat)
}

func TestConnect_ForwardsParsedFormat(t *testing.T) {
	ctx := context.Background()
	enc := testEncryptor(t)
	businessID := uuid.New()

	repo := &mockIntegrationRepository{
		createFunc: func(_ context.Context, integration *domain.Integration) error {
			integration.ID = uuid.New()
			return nil
		},
	}

	rec := &recordingSyncLogger{}
	svc := NewIntegrationService(repo, testEnvelope(t, enc), nil, nil, rec)

	_, err := svc.Connect(ctx, ConnectParams{
		BusinessID:  businessID,
		Platform:    "telegram",
		ExternalID:  "ext_2",
		AccessToken: "tok",
	})
	require.NoError(t, err)

	require.Len(t, rec.asyncCalls, 1)
	var details audit.IntegrationConnectedDetails
	require.NoError(t, json.Unmarshal(rec.asyncCalls[0].Details, &details))
	assert.Empty(t, details.ActorIP)
	assert.Empty(t, details.UserAgent)
	assert.Empty(t, details.ParsedFormat)
}

// TestConnect_IdempotentReconnect_SoftDeletesExistingRow — reconnecting the
// same channel must retire the live row first (so the partial-unique index
// lets the fresh active row replace a token_expired one), then create anew.
func TestConnect_IdempotentReconnect_SoftDeletesExistingRow(t *testing.T) {
	ctx := context.Background()
	enc := testEncryptor(t)

	businessID := uuid.New()
	externalID := "ext_telegram_reconnect"
	existingID := uuid.New()

	var softDeletedID uuid.UUID
	var created bool
	repo := &mockIntegrationRepository{
		getByBusinessPlatformExternalFunc: func(_ context.Context, bid uuid.UUID, plat, extID string) (*domain.Integration, error) {
			if bid == businessID && plat == "telegram" && extID == externalID {
				return &domain.Integration{
					ID:         existingID,
					BusinessID: businessID,
					Platform:   "telegram",
					ExternalID: externalID,
					Status:     domain.IntegrationStatusTokenExpired,
				}, nil
			}
			return nil, domain.ErrIntegrationNotFound
		},
		softDeleteFunc: func(_ context.Context, id uuid.UUID) error {
			softDeletedID = id
			return nil
		},
		createFunc: func(_ context.Context, integration *domain.Integration) error {
			created = true
			assert.Equal(t, domain.IntegrationStatusActive, integration.Status)
			return nil
		},
	}

	svc := NewIntegrationService(repo, testEnvelope(t, enc), nil, nil, audit.Nop())
	result, err := svc.Connect(ctx, ConnectParams{
		BusinessID:  businessID,
		Platform:    "telegram",
		ExternalID:  externalID,
		AccessToken: "fresh_token",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, existingID, softDeletedID, "the existing live row must be soft-deleted before create")
	assert.True(t, created, "a fresh active row must be created")
	assert.Equal(t, domain.IntegrationStatusActive, result.Status)
}

// TestConnect_NoExistingRow_SkipsSoftDelete — a first-time connect (no live
// row) must not attempt a soft-delete; ErrIntegrationNotFound is the normal
// no-op path.
func TestConnect_NoExistingRow_SkipsSoftDelete(t *testing.T) {
	ctx := context.Background()
	enc := testEncryptor(t)

	softDeleteCalled := false
	repo := &mockIntegrationRepository{
		getByBusinessPlatformExternalFunc: func(_ context.Context, _ uuid.UUID, _, _ string) (*domain.Integration, error) {
			return nil, domain.ErrIntegrationNotFound
		},
		softDeleteFunc: func(_ context.Context, _ uuid.UUID) error {
			softDeleteCalled = true
			return nil
		},
	}

	svc := NewIntegrationService(repo, testEnvelope(t, enc), nil, nil, audit.Nop())
	_, err := svc.Connect(ctx, ConnectParams{
		BusinessID:  uuid.New(),
		Platform:    "telegram",
		ExternalID:  "ext_new",
		AccessToken: "token",
	})

	require.NoError(t, err)
	assert.False(t, softDeleteCalled, "soft-delete must not run when no live row exists")
}

func TestConnect_Duplicate(t *testing.T) {
	ctx := context.Background()
	enc := testEncryptor(t)

	repo := &mockIntegrationRepository{
		createFunc: func(ctx context.Context, integration *domain.Integration) error {
			return domain.ErrIntegrationExists
		},
	}

	svc := NewIntegrationService(repo, testEnvelope(t, enc), nil, nil, audit.Nop())
	params := ConnectParams{
		BusinessID:  uuid.New(),
		Platform:    "telegram",
		ExternalID:  "ext_123",
		AccessToken: "token",
	}
	result, err := svc.Connect(ctx, params)

	assert.Nil(t, result)
	assert.ErrorIs(t, err, domain.ErrIntegrationExists)
}

func TestConnect_MissingBusinessID(t *testing.T) {
	ctx := context.Background()
	enc := testEncryptor(t)

	repo := &mockIntegrationRepository{}
	svc := NewIntegrationService(repo, testEnvelope(t, enc), nil, nil, audit.Nop())
	params := ConnectParams{
		BusinessID:  uuid.Nil,
		Platform:    "telegram",
		ExternalID:  "ext_123",
		AccessToken: "token",
	}
	result, err := svc.Connect(ctx, params)

	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "business id is required")
}

func TestConnect_MissingPlatform(t *testing.T) {
	ctx := context.Background()
	enc := testEncryptor(t)

	repo := &mockIntegrationRepository{}
	svc := NewIntegrationService(repo, testEnvelope(t, enc), nil, nil, audit.Nop())
	params := ConnectParams{
		BusinessID:  uuid.New(),
		Platform:    "",
		ExternalID:  "ext_123",
		AccessToken: "token",
	}
	result, err := svc.Connect(ctx, params)

	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "platform is required")
}

func TestGetDecryptedToken_Success(t *testing.T) {
	ctx := context.Background()
	enc := testEncryptor(t)

	businessID := uuid.New()
	platform := "vk"
	externalID := "vk_user_999"
	plaintext := "plaintext_access_token"

	encryptedToken, err := enc.Encrypt([]byte(plaintext))
	require.NoError(t, err)

	integration := &domain.Integration{
		ID:                   uuid.New(),
		BusinessID:           businessID,
		Platform:             platform,
		ExternalID:           externalID,
		Status:               "active",
		EncryptedAccessToken: encryptedToken,
		Metadata:             map[string]interface{}{"group_id": "123"},
	}

	repo := &mockIntegrationRepository{
		getByBusinessPlatformExternalFunc: func(ctx context.Context, bid uuid.UUID, plat string, extID string) (*domain.Integration, error) {
			if bid == businessID && plat == platform && extID == externalID {
				return integration, nil
			}
			return nil, domain.ErrIntegrationNotFound
		},
	}

	svc := NewIntegrationService(repo, testEnvelope(t, enc), nil, nil, audit.Nop())
	resp, err := svc.GetDecryptedToken(ctx, businessID, platform, externalID, "test")

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, plaintext, resp.AccessToken)
	assert.Equal(t, platform, resp.Platform)
	assert.Equal(t, externalID, resp.ExternalID)
	assert.Equal(t, integration.ID, resp.IntegrationID)
}

func TestGetDecryptedToken_NotFound(t *testing.T) {
	ctx := context.Background()
	enc := testEncryptor(t)

	repo := &mockIntegrationRepository{
		getByBusinessPlatformExternalFunc: func(ctx context.Context, bid uuid.UUID, plat string, extID string) (*domain.Integration, error) {
			return nil, domain.ErrIntegrationNotFound
		},
	}

	svc := NewIntegrationService(repo, testEnvelope(t, enc), nil, nil, audit.Nop())
	resp, err := svc.GetDecryptedToken(ctx, uuid.New(), "telegram", "ext_999", "test")

	assert.Nil(t, resp)
	assert.ErrorIs(t, err, domain.ErrIntegrationNotFound)
}

func TestGetDecryptedToken_Expired(t *testing.T) {
	ctx := context.Background()
	enc := testEncryptor(t)

	businessID := uuid.New()
	platform := "vk"
	externalID := "vk_user_expired"

	past := time.Now().Add(-1 * time.Hour)
	integration := &domain.Integration{
		ID:                    uuid.New(),
		BusinessID:            businessID,
		Platform:              platform,
		ExternalID:            externalID,
		Status:                "active",
		EncryptedAccessToken:  []byte("some_encrypted_bytes"),
		EncryptedRefreshToken: nil,
		TokenExpiresAt:        &past,
	}

	repo := &mockIntegrationRepository{
		getByBusinessPlatformExternalFunc: func(ctx context.Context, bid uuid.UUID, plat string, extID string) (*domain.Integration, error) {
			return integration, nil
		},
	}

	svc := NewIntegrationService(repo, testEnvelope(t, enc), nil, nil, audit.Nop())
	resp, err := svc.GetDecryptedToken(ctx, businessID, platform, externalID, "test")

	assert.Nil(t, resp)
	assert.ErrorIs(t, err, domain.ErrTokenExpired)
}

func TestListByBusinessAndPlatform_Success(t *testing.T) {
	ctx := context.Background()
	enc := testEncryptor(t)

	businessID := uuid.New()
	platform := "telegram"
	integrations := []domain.Integration{
		{
			ID:         uuid.New(),
			BusinessID: businessID,
			Platform:   platform,
			ExternalID: "chan_1",
			Status:     "active",
		},
		{
			ID:         uuid.New(),
			BusinessID: businessID,
			Platform:   platform,
			ExternalID: "chan_2",
			Status:     "active",
		},
	}

	repo := &mockIntegrationRepository{
		listByBusinessAndPlatformFunc: func(ctx context.Context, bid uuid.UUID, plat string) ([]domain.Integration, error) {
			if bid == businessID && plat == platform {
				return integrations, nil
			}
			return []domain.Integration{}, nil
		},
	}

	svc := NewIntegrationService(repo, testEnvelope(t, enc), nil, nil, audit.Nop())
	result, err := svc.ListByBusinessAndPlatform(ctx, businessID, platform)

	require.NoError(t, err)
	assert.Len(t, result, 2)
	assert.Equal(t, "chan_1", result[0].ExternalID)
	assert.Equal(t, "chan_2", result[1].ExternalID)
}

func TestMarkTokenExpired_DelegatesToRepo(t *testing.T) {
	ctx := context.Background()
	enc := testEncryptor(t)

	tests := []struct {
		name       string
		externalID string
	}{
		{name: "scoped to one integration", externalID: "chan_1001"},
		{name: "platform-wide fallback when external id unknown", externalID: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			businessID := uuid.New()
			var gotBusinessID uuid.UUID
			var gotPlatform, gotExternalID string
			repo := &mockIntegrationRepository{
				markTokenExpiredFunc: func(_ context.Context, bid uuid.UUID, plat, ext string) (int64, error) {
					gotBusinessID = bid
					gotPlatform = plat
					gotExternalID = ext
					return 1, nil
				},
			}

			svc := NewIntegrationService(repo, testEnvelope(t, enc), nil, nil, audit.Nop())
			err := svc.MarkTokenExpired(ctx, businessID, "telegram", tt.externalID)

			require.NoError(t, err)
			assert.Equal(t, businessID, gotBusinessID)
			assert.Equal(t, "telegram", gotPlatform)
			assert.Equal(t, tt.externalID, gotExternalID)
		})
	}
}

func TestMarkTokenExpired_Validation(t *testing.T) {
	ctx := context.Background()
	enc := testEncryptor(t)
	svc := NewIntegrationService(&mockIntegrationRepository{}, testEnvelope(t, enc), nil, nil, audit.Nop())

	assert.ErrorContains(t, svc.MarkTokenExpired(ctx, uuid.Nil, "telegram", "chan_1"), "business id is required")
	assert.ErrorContains(t, svc.MarkTokenExpired(ctx, uuid.New(), "", "chan_1"), "platform is required")
}

func TestListByBusinessAndPlatform_NilBusinessID(t *testing.T) {
	ctx := context.Background()
	enc := testEncryptor(t)

	repo := &mockIntegrationRepository{}
	svc := NewIntegrationService(repo, testEnvelope(t, enc), nil, nil, audit.Nop())

	result, err := svc.ListByBusinessAndPlatform(ctx, uuid.Nil, "telegram")

	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "business id is required")
}

// --- mockTokenRefresher ---

type mockTokenRefresher struct {
	refreshFunc func(ctx context.Context, refreshToken string) (string, string, int64, error)
	callCount   int
}

func (m *mockTokenRefresher) RefreshToken(ctx context.Context, refreshToken string) (accessToken, newRefreshToken string, expiresIn int64, err error) {
	m.callCount++
	if m.refreshFunc != nil {
		return m.refreshFunc(ctx, refreshToken)
	}
	return "", "", 0, fmt.Errorf("not implemented")
}

// --- Token refresh tests ---

func TestGetDecryptedToken_RefreshesExpiredGoogleToken(t *testing.T) {
	ctx := context.Background()
	enc := testEncryptor(t)

	businessID := uuid.New()
	integrationID := uuid.New()
	platform := "google_business"
	externalID := "locations/12345"

	oldAccess := "old_access_token"
	refreshTokenPlain := "my_refresh_token"
	newAccess := "new_access_token"

	encOldAccess, err := enc.Encrypt([]byte(oldAccess))
	require.NoError(t, err)
	encRefresh, err := enc.Encrypt([]byte(refreshTokenPlain))
	require.NoError(t, err)

	past := time.Now().Add(-1 * time.Hour)
	integration := &domain.Integration{
		ID:                    integrationID,
		BusinessID:            businessID,
		Platform:              platform,
		ExternalID:            externalID,
		Status:                "active",
		EncryptedAccessToken:  encOldAccess,
		EncryptedRefreshToken: encRefresh,
		TokenExpiresAt:        &past,
	}

	var updatedIntegration *domain.Integration
	repo := &mockIntegrationRepository{
		getByBusinessPlatformExternalFunc: func(ctx context.Context, bid uuid.UUID, plat string, extID string) (*domain.Integration, error) {
			return integration, nil
		},
		getByIDFunc: func(ctx context.Context, id uuid.UUID) (*domain.Integration, error) {
			return integration, nil
		},
		updateFunc: func(ctx context.Context, i *domain.Integration) error {
			updatedIntegration = i
			return nil
		},
	}

	refresher := &mockTokenRefresher{
		refreshFunc: func(ctx context.Context, rt string) (string, string, int64, error) {
			assert.Equal(t, refreshTokenPlain, rt)
			return newAccess, "", 3600, nil
		},
	}

	svc := NewIntegrationService(repo, testEnvelope(t, enc), nil, refresher, audit.Nop())
	resp, err := svc.GetDecryptedToken(ctx, businessID, platform, externalID, "test")

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, newAccess, resp.AccessToken)
	assert.Equal(t, 1, refresher.callCount)

	require.NotNil(t, updatedIntegration)
	decAccess, err := enc.Decrypt(updatedIntegration.EncryptedAccessToken)
	require.NoError(t, err)
	assert.Equal(t, newAccess, string(decAccess))

	require.NotNil(t, updatedIntegration.TokenExpiresAt)
	assert.True(t, updatedIntegration.TokenExpiresAt.After(time.Now()))
}

func TestGetDecryptedToken_RefreshRotatesRefreshToken(t *testing.T) {
	ctx := context.Background()
	enc := testEncryptor(t)

	businessID := uuid.New()
	integrationID := uuid.New()

	refreshTokenPlain := "old_refresh_token"
	newRefreshPlain := "new_rotated_refresh_token"
	newAccess := "refreshed_access"

	encAccess, err := enc.Encrypt([]byte("expired_access"))
	require.NoError(t, err)
	encRefresh, err := enc.Encrypt([]byte(refreshTokenPlain))
	require.NoError(t, err)

	past := time.Now().Add(-30 * time.Minute)
	integration := &domain.Integration{
		ID:                    integrationID,
		BusinessID:            businessID,
		Platform:              "google_business",
		ExternalID:            "locations/99",
		Status:                "active",
		EncryptedAccessToken:  encAccess,
		EncryptedRefreshToken: encRefresh,
		TokenExpiresAt:        &past,
	}

	var updatedIntegration *domain.Integration
	repo := &mockIntegrationRepository{
		getByBusinessPlatformExternalFunc: func(ctx context.Context, bid uuid.UUID, plat string, extID string) (*domain.Integration, error) {
			return integration, nil
		},
		getByIDFunc: func(ctx context.Context, id uuid.UUID) (*domain.Integration, error) {
			return integration, nil
		},
		updateFunc: func(ctx context.Context, i *domain.Integration) error {
			updatedIntegration = i
			return nil
		},
	}

	refresher := &mockTokenRefresher{
		refreshFunc: func(ctx context.Context, rt string) (string, string, int64, error) {
			return newAccess, newRefreshPlain, 3600, nil
		},
	}

	svc := NewIntegrationService(repo, testEnvelope(t, enc), nil, refresher, audit.Nop())
	resp, err := svc.GetDecryptedToken(ctx, businessID, "google_business", "locations/99", "test")

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, newAccess, resp.AccessToken)

	require.NotNil(t, updatedIntegration)
	decRefresh, err := enc.Decrypt(updatedIntegration.EncryptedRefreshToken)
	require.NoError(t, err)
	assert.Equal(t, newRefreshPlain, string(decRefresh))
}

func TestGetDecryptedToken_ExpiredNoRefresher_ReturnsError(t *testing.T) {
	ctx := context.Background()
	enc := testEncryptor(t)

	businessID := uuid.New()

	encAccess, err := enc.Encrypt([]byte("some_token"))
	require.NoError(t, err)
	encRefresh, err := enc.Encrypt([]byte("refresh"))
	require.NoError(t, err)

	past := time.Now().Add(-1 * time.Hour)
	integration := &domain.Integration{
		ID:                    uuid.New(),
		BusinessID:            businessID,
		Platform:              "google_business",
		ExternalID:            "loc/1",
		Status:                "active",
		EncryptedAccessToken:  encAccess,
		EncryptedRefreshToken: encRefresh,
		TokenExpiresAt:        &past,
	}

	repo := &mockIntegrationRepository{
		getByBusinessPlatformExternalFunc: func(ctx context.Context, bid uuid.UUID, plat string, extID string) (*domain.Integration, error) {
			return integration, nil
		},
	}

	svc := NewIntegrationService(repo, testEnvelope(t, enc), nil, nil, audit.Nop())
	resp, err := svc.GetDecryptedToken(ctx, businessID, "google_business", "loc/1", "test")

	assert.Nil(t, resp)
	assert.ErrorIs(t, err, domain.ErrTokenExpired)
}

func TestGetDecryptedToken_ExpiredNoRefreshToken_ReturnsError(t *testing.T) {
	ctx := context.Background()
	enc := testEncryptor(t)

	businessID := uuid.New()

	past := time.Now().Add(-1 * time.Hour)
	integration := &domain.Integration{
		ID:                    uuid.New(),
		BusinessID:            businessID,
		Platform:              "google_business",
		ExternalID:            "loc/1",
		Status:                "active",
		EncryptedAccessToken:  []byte("some_encrypted_bytes"),
		EncryptedRefreshToken: nil,
		TokenExpiresAt:        &past,
	}

	repo := &mockIntegrationRepository{
		getByBusinessPlatformExternalFunc: func(ctx context.Context, bid uuid.UUID, plat string, extID string) (*domain.Integration, error) {
			return integration, nil
		},
	}

	refresher := &mockTokenRefresher{}
	svc := NewIntegrationService(repo, testEnvelope(t, enc), nil, refresher, audit.Nop())
	resp, err := svc.GetDecryptedToken(ctx, businessID, "google_business", "loc/1", "test")

	assert.Nil(t, resp)
	assert.ErrorIs(t, err, domain.ErrTokenExpired)
	assert.Equal(t, 0, refresher.callCount, "refresher should not be called when no refresh token")
}

func TestGetDecryptedToken_NotExpired_NoRefresh(t *testing.T) {
	ctx := context.Background()
	enc := testEncryptor(t)

	businessID := uuid.New()
	plaintext := "valid_access_token"

	encAccess, err := enc.Encrypt([]byte(plaintext))
	require.NoError(t, err)

	future := time.Now().Add(1 * time.Hour)
	integration := &domain.Integration{
		ID:                   uuid.New(),
		BusinessID:           businessID,
		Platform:             "google_business",
		ExternalID:           "loc/2",
		Status:               "active",
		EncryptedAccessToken: encAccess,
		TokenExpiresAt:       &future,
	}

	repo := &mockIntegrationRepository{
		getByBusinessPlatformExternalFunc: func(ctx context.Context, bid uuid.UUID, plat string, extID string) (*domain.Integration, error) {
			return integration, nil
		},
	}

	refresher := &mockTokenRefresher{}
	svc := NewIntegrationService(repo, testEnvelope(t, enc), nil, refresher, audit.Nop())
	resp, err := svc.GetDecryptedToken(ctx, businessID, "google_business", "loc/2", "test")

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, plaintext, resp.AccessToken)
	assert.Equal(t, 0, refresher.callCount, "should not refresh non-expired token")
}

func TestGetDecryptedToken_ConcurrentRefresh_OnlyOneCall(t *testing.T) {
	ctx := context.Background()
	enc := testEncryptor(t)

	businessID := uuid.New()
	integrationID := uuid.New()

	refreshTokenPlain := "my_refresh"
	newAccess := "refreshed_token"

	encAccess, err := enc.Encrypt([]byte("old"))
	require.NoError(t, err)
	encRefresh, err := enc.Encrypt([]byte(refreshTokenPlain))
	require.NoError(t, err)

	past := time.Now().Add(-1 * time.Hour)

	refreshed := false
	newEncAccess, _ := enc.Encrypt([]byte(newAccess))
	future := time.Now().Add(1 * time.Hour)

	repo := &mockIntegrationRepository{
		getByBusinessPlatformExternalFunc: func(ctx context.Context, bid uuid.UUID, plat string, extID string) (*domain.Integration, error) {
			return &domain.Integration{
				ID:                    integrationID,
				BusinessID:            businessID,
				Platform:              "google_business",
				ExternalID:            "loc/1",
				Status:                "active",
				EncryptedAccessToken:  encAccess,
				EncryptedRefreshToken: encRefresh,
				TokenExpiresAt:        &past,
			}, nil
		},
		getByIDFunc: func(ctx context.Context, id uuid.UUID) (*domain.Integration, error) {
			if refreshed {
				return &domain.Integration{
					ID:                    integrationID,
					BusinessID:            businessID,
					Platform:              "google_business",
					ExternalID:            "loc/1",
					Status:                "active",
					EncryptedAccessToken:  newEncAccess,
					EncryptedRefreshToken: encRefresh,
					TokenExpiresAt:        &future,
				}, nil
			}
			return &domain.Integration{
				ID:                    integrationID,
				BusinessID:            businessID,
				Platform:              "google_business",
				ExternalID:            "loc/1",
				Status:                "active",
				EncryptedAccessToken:  encAccess,
				EncryptedRefreshToken: encRefresh,
				TokenExpiresAt:        &past,
			}, nil
		},
		updateFunc: func(ctx context.Context, i *domain.Integration) error {
			refreshed = true
			return nil
		},
	}

	refresher := &mockTokenRefresher{
		refreshFunc: func(ctx context.Context, rt string) (string, string, int64, error) {
			return newAccess, "", 3600, nil
		},
	}

	svc := NewIntegrationService(repo, testEnvelope(t, enc), nil, refresher, audit.Nop())

	resp1, err := svc.GetDecryptedToken(ctx, businessID, "google_business", "loc/1", "test")
	require.NoError(t, err)
	assert.Equal(t, newAccess, resp1.AccessToken)

	resp2, err := svc.GetDecryptedToken(ctx, businessID, "google_business", "loc/1", "test")
	require.NoError(t, err)
	assert.Equal(t, newAccess, resp2.AccessToken)

	assert.Equal(t, 1, refresher.callCount, "should only refresh once due to mutex + double-check")
}

type recordingSyncLogger struct {
	syncCalls   []audit.Entry
	asyncCalls  []audit.Entry
	logSyncErr  error
	logSyncSeen bool
}

func (r *recordingSyncLogger) Log(_ context.Context, e audit.Entry) {
	r.asyncCalls = append(r.asyncCalls, e)
}

func (r *recordingSyncLogger) LogSync(_ context.Context, e audit.Entry) error {
	r.logSyncSeen = true
	r.syncCalls = append(r.syncCalls, e)
	return r.logSyncErr
}

func tokenTestIntegration(t *testing.T, businessID uuid.UUID, platform, externalID string) (*domain.Integration, *crypto.Encryptor) {
	t.Helper()
	enc := testEncryptor(t)
	encryptedToken, err := enc.Encrypt([]byte("plaintext_access_token"))
	require.NoError(t, err)
	return &domain.Integration{
		ID:                   uuid.New(),
		BusinessID:           businessID,
		Platform:             platform,
		ExternalID:           externalID,
		Status:               "active",
		EncryptedAccessToken: encryptedToken,
	}, enc
}

func TestGetDecryptedToken_EmitsAuditRow(t *testing.T) {
	ctx := context.Background()
	businessID := uuid.New()
	integration, enc := tokenTestIntegration(t, businessID, "vk", "vk_999")

	repo := &mockIntegrationRepository{
		getByBusinessPlatformExternalFunc: func(_ context.Context, _ uuid.UUID, _, _ string) (*domain.Integration, error) {
			return integration, nil
		},
	}

	rec := &recordingSyncLogger{}
	svc := NewIntegrationService(repo, testEnvelope(t, enc), nil, nil, rec)

	resp, err := svc.GetDecryptedToken(ctx, businessID, "vk", "vk_999", "vk_post")
	require.NoError(t, err)
	require.NotNil(t, resp)

	require.True(t, rec.logSyncSeen, "audit LogSync must be invoked before returning the token")
	require.Len(t, rec.syncCalls, 1)
	entry := rec.syncCalls[0]
	assert.Equal(t, audit.ActionIntegrationTokenDecrypted, entry.Action)
	require.NotNil(t, entry.BusinessID)
	assert.Equal(t, businessID, *entry.BusinessID)

	var details audit.TokenDecryptedDetails
	require.NoError(t, json.Unmarshal(entry.Details, &details))
	assert.Equal(t, integration.ID, details.IntegrationID)
	assert.Equal(t, "vk", details.Platform)
	assert.Equal(t, "api.internal", details.CallerService)
	assert.Equal(t, "vk_post", details.Reason)
}

func TestGetDecryptedToken_AuditInsertFails_NoToken(t *testing.T) {
	ctx := context.Background()
	businessID := uuid.New()
	integration, enc := tokenTestIntegration(t, businessID, "vk", "vk_999")

	repo := &mockIntegrationRepository{
		getByBusinessPlatformExternalFunc: func(_ context.Context, _ uuid.UUID, _, _ string) (*domain.Integration, error) {
			return integration, nil
		},
	}

	sentinel := errors.New("audit insert exploded")
	rec := &recordingSyncLogger{logSyncErr: sentinel}
	svc := NewIntegrationService(repo, testEnvelope(t, enc), nil, nil, rec)

	resp, err := svc.GetDecryptedToken(ctx, businessID, "vk", "vk_999", "vk_post")
	require.Error(t, err)
	assert.Nil(t, resp, "token must not be returned when the audit INSERT fails")
	assert.ErrorIs(t, err, sentinel)
}

func TestGetDecryptedToken_CallerFromMTLSCN(t *testing.T) {
	businessID := uuid.New()
	integration, enc := tokenTestIntegration(t, businessID, "telegram", "chan_1")

	repo := &mockIntegrationRepository{
		getByBusinessPlatformExternalFunc: func(_ context.Context, _ uuid.UUID, _, _ string) (*domain.Integration, error) {
			return integration, nil
		},
	}

	t.Run("identity from mTLS CN", func(t *testing.T) {
		rec := &recordingSyncLogger{}
		svc := NewIntegrationService(repo, testEnvelope(t, enc), nil, nil, rec)
		ctx := middleware.WithServiceIdentity(context.Background(), "agent-telegram")

		_, err := svc.GetDecryptedToken(ctx, businessID, "telegram", "chan_1", "telegram_notify")
		require.NoError(t, err)
		require.Len(t, rec.syncCalls, 1)

		var details audit.TokenDecryptedDetails
		require.NoError(t, json.Unmarshal(rec.syncCalls[0].Details, &details))
		assert.Equal(t, "agent-telegram", details.CallerService)
	})

	t.Run("falls back to api.internal", func(t *testing.T) {
		rec := &recordingSyncLogger{}
		svc := NewIntegrationService(repo, testEnvelope(t, enc), nil, nil, rec)

		_, err := svc.GetDecryptedToken(context.Background(), businessID, "telegram", "chan_1", "telegram_notify")
		require.NoError(t, err)
		require.Len(t, rec.syncCalls, 1)

		var details audit.TokenDecryptedDetails
		require.NoError(t, json.Unmarshal(rec.syncCalls[0].Details, &details))
		assert.Equal(t, "api.internal", details.CallerService)
	})
}

func TestGetDecryptedToken_MetricOnSuccessNotOnAuditFailure(t *testing.T) {
	businessID := uuid.New()
	integration, enc := tokenTestIntegration(t, businessID, "vk", "vk_999")

	repo := &mockIntegrationRepository{
		getByBusinessPlatformExternalFunc: func(_ context.Context, _ uuid.UUID, _, _ string) (*domain.Integration, error) {
			return integration, nil
		},
	}

	t.Run("success increments metric", func(t *testing.T) {
		before := testutil.ToFloat64(metrics.IntegrationTokenDecryptedCounter("vk", "api.internal"))
		rec := &recordingSyncLogger{}
		svc := NewIntegrationService(repo, testEnvelope(t, enc), nil, nil, rec)

		_, err := svc.GetDecryptedToken(context.Background(), businessID, "vk", "vk_999", "vk_post")
		require.NoError(t, err)

		after := testutil.ToFloat64(metrics.IntegrationTokenDecryptedCounter("vk", "api.internal"))
		assert.InDelta(t, before+1, after, 0.0001, "metric must increment once on success")
	})

	t.Run("audit failure does not increment metric", func(t *testing.T) {
		before := testutil.ToFloat64(metrics.IntegrationTokenDecryptedCounter("vk", "api.internal"))
		rec := &recordingSyncLogger{logSyncErr: errors.New("boom")}
		svc := NewIntegrationService(repo, testEnvelope(t, enc), nil, nil, rec)

		_, err := svc.GetDecryptedToken(context.Background(), businessID, "vk", "vk_999", "vk_post")
		require.Error(t, err)

		after := testutil.ToFloat64(metrics.IntegrationTokenDecryptedCounter("vk", "api.internal"))
		assert.InDelta(t, before, after, 0.0001, "metric must not increment when audit fails")
	})
}

// TestGetDecryptedToken_RefreshEmptyRefresh_KMS_NoDEKMismatch is a regression
// guard for the envelope DEK-mismatch bug: on a routine refresh Google returns a
// new access token but an EMPTY refresh token. EncryptForRow mints a brand-new
// per-row DEK, so every carried-forward field (refresh AND user token) must be
// re-sealed under that DEK; leaving any field sealed under the old DEK bricks the
// integration on the next decrypt. The integration must survive two refresh
// cycles with all fields still decryptable.
func TestGetDecryptedToken_RefreshEmptyRefresh_KMS_NoDEKMismatch(t *testing.T) {
	ctx := context.Background()
	env := testKMSEnvelope(t)

	businessID := uuid.New()
	integrationID := uuid.New()
	platform := "google_business"
	externalID := "locations/4242"

	refreshPlain := "the_refresh_token"
	userPlain := "the_vk_user_token"

	pts := [][]byte{[]byte("expired_access"), []byte(refreshPlain), []byte(userPlain)}
	cts, wrapped, ver, fp, err := env.EncryptForRow(ctx, integrationID, platform, pts)
	require.NoError(t, err)

	past := time.Now().Add(-1 * time.Hour)
	future := time.Now().Add(1 * time.Hour)
	row := &domain.Integration{
		ID:                       integrationID,
		BusinessID:               businessID,
		Platform:                 platform,
		ExternalID:               externalID,
		Status:                   "active",
		EncryptedAccessToken:     cts[0],
		EncryptedRefreshToken:    cts[1],
		EncryptedUserToken:       cts[2],
		WrappedDEK:               wrapped,
		KeyVersion:               ver,
		EncryptionKeyFingerprint: fp,
		UserTokenExpiresAt:       &future,
		TokenExpiresAt:           &past,
	}

	repo := &mockIntegrationRepository{
		getByBusinessPlatformExternalFunc: func(_ context.Context, _ uuid.UUID, _, _ string) (*domain.Integration, error) {
			return row, nil
		},
		getByIDFunc: func(_ context.Context, _ uuid.UUID) (*domain.Integration, error) {
			return row, nil
		},
		updateFunc: func(_ context.Context, i *domain.Integration) error {
			row = i
			return nil
		},
	}

	refresher := &mockTokenRefresher{
		refreshFunc: func(_ context.Context, rt string) (string, string, int64, error) {
			assert.Equal(t, refreshPlain, rt, "refresher must receive the carried-forward refresh token")
			return "new_access_" + uuid.NewString(), "", 3600, nil
		},
	}

	svc := NewIntegrationService(repo, env, nil, refresher, audit.Nop())

	resp, err := svc.GetDecryptedToken(ctx, businessID, platform, externalID, "test")
	require.NoError(t, err, "first refresh must not brick the integration via DEK mismatch")
	require.NotNil(t, resp)
	assert.NotEmpty(t, resp.AccessToken)
	assert.Equal(t, userPlain, resp.UserToken, "user token must survive the re-seal under the new DEK")

	row.TokenExpiresAt = &past

	resp2, err := svc.GetDecryptedToken(ctx, businessID, platform, externalID, "test")
	require.NoError(t, err, "second refresh cycle must still decrypt — integration not bricked")
	require.NotNil(t, resp2)
	assert.NotEmpty(t, resp2.AccessToken)
	assert.Equal(t, userPlain, resp2.UserToken)
	assert.Equal(t, 2, refresher.callCount)
}

func TestWipeRefreshPlaintexts_RotatedZeroesNewRefresh(t *testing.T) {
	newAccess := []byte("fresh-access-token")
	rotatedRefresh := []byte("fresh-rotated-refresh-token")
	userToken := []byte("user-token")
	pts := [][]byte{newAccess, rotatedRefresh, userToken}

	wipeRefreshPlaintexts(pts, true)

	assert.Equal(t, make([]byte, len(newAccess)), pts[0], "new access plaintext must be zeroed")
	assert.Equal(t, make([]byte, len(rotatedRefresh)), pts[1],
		"rotated refresh plaintext must be zeroed; without this the secret lingers in memory")
}

func TestWipeRefreshPlaintexts_NotRotatedLeavesRefreshAlias(t *testing.T) {
	newAccess := []byte("fresh-access-token")
	aliasedRefresh := []byte("old-refresh-still-aliased-by-refreshPts")
	original := append([]byte(nil), aliasedRefresh...)
	pts := [][]byte{newAccess, aliasedRefresh, nil}

	wipeRefreshPlaintexts(pts, false)

	assert.Equal(t, make([]byte, len(newAccess)), pts[0], "new access plaintext must be zeroed")
	assert.Equal(t, original, pts[1],
		"non-rotated refresh aliases refreshPts and must be left for its own deferred wipe")
}

// stubActorLookup is a minimal ActorLookup that returns a fixed user (or
// ErrUserNotFound when nil) for the connect-actor gate tests.
type stubActorLookup struct {
	user *domain.User
	err  error
}

func (s stubActorLookup) GetByID(_ context.Context, _ uuid.UUID) (*domain.User, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.user, nil
}

// TestConnect_ActorGate is the fail-on-revert guard for the OAuth-callback
// gating bypass: integrationService.Connect, when a WithActorGate lookup is
// attached, must re-assert the same email-verified and account-deletion-grace
// predicates the RequireVerifiedEmailDay0 / BlockWritesDuringGrace middlewares
// enforce — because the public OAuth callbacks (Yandex true-OAuth, Google
// single-location) persist a live integration outside those middlewares. An
// unverified actor, or an actor mid-deletion, must be rejected BEFORE the row
// is created; a verified, non-deleting actor still connects.
func TestConnect_ActorGate(t *testing.T) {
	ctx := context.Background()
	actorID := uuid.New()
	now := time.Now()
	requestedAt := now.Add(-24 * time.Hour)
	canceledAt := now.Add(-time.Hour)

	baseParams := func() ConnectParams {
		return ConnectParams{
			BusinessID:   uuid.New(),
			ActorID:      actorID,
			Platform:     "yandex_business",
			ExternalID:   "default",
			AccessToken:  "tok",
			ParsedFormat: ParsedFormatOAuthCode,
		}
	}

	tests := []struct {
		name        string
		user        *domain.User
		lookupErr   error
		wantErr     error
		wantCreated bool
	}{
		{
			name:        "unverified actor is rejected before persist",
			user:        &domain.User{ID: actorID, EmailVerified: false},
			wantErr:     domain.ErrActorEmailNotVerified,
			wantCreated: false,
		},
		{
			name:        "pending-deletion actor is rejected before persist",
			user:        &domain.User{ID: actorID, EmailVerified: true, DeletionRequestedAt: &requestedAt},
			wantErr:     domain.ErrActorPendingDeletion,
			wantCreated: false,
		},
		{
			name:        "verified non-deleting actor connects",
			user:        &domain.User{ID: actorID, EmailVerified: true},
			wantCreated: true,
		},
		{
			name:        "canceled deletion is not pending and connects",
			user:        &domain.User{ID: actorID, EmailVerified: true, DeletionRequestedAt: &requestedAt, DeletionCanceledAt: &canceledAt},
			wantCreated: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var created bool
			repo := &mockIntegrationRepository{
				createFunc: func(_ context.Context, integration *domain.Integration) error {
					created = true
					integration.ID = uuid.New()
					return nil
				},
			}

			svc := NewIntegrationService(repo, testEnvelope(t, testEncryptor(t)), nil, nil, audit.Nop())
			svc = WithActorGate(svc, stubActorLookup{user: tc.user, err: tc.lookupErr})

			_, err := svc.Connect(ctx, baseParams())

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.wantCreated, created,
				"repo.Create call expectation mismatch — the gate must reject before persisting")
		})
	}
}
