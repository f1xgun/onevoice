package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/crypto"
	"github.com/f1xgun/onevoice/pkg/domain"
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

// testEncryptor creates a test encryptor with a 32-byte key
func testEncryptor(t *testing.T) *crypto.Encryptor {
	t.Helper()
	testKey := []byte("12345678901234567890123456789012") // 32 bytes
	enc, err := crypto.NewEncryptor(testKey)
	require.NoError(t, err)
	return enc
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

		svc := NewIntegrationService(repo, testEncryptor(t), nil)
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

		svc := NewIntegrationService(repo, testEncryptor(t), nil)
		result, err := svc.ListByBusinessID(ctx, businessID)

		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Len(t, result, 0)
	})

	t.Run("error - nil business id", func(t *testing.T) {
		repo := &mockIntegrationRepository{}
		svc := NewIntegrationService(repo, testEncryptor(t), nil)

		result, err := svc.ListByBusinessID(ctx, uuid.Nil)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "business id is required")
	})

	t.Run("error - canceled context", func(t *testing.T) {
		cancelledCtx, cancel := context.WithCancel(context.Background())
		cancel()

		repo := &mockIntegrationRepository{}
		svc := NewIntegrationService(repo, testEncryptor(t), nil)

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

		svc := NewIntegrationService(repo, testEncryptor(t), nil)
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

		svc := NewIntegrationService(repo, testEncryptor(t), nil)
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

		svc := NewIntegrationService(repo, testEncryptor(t), nil)
		result, err := svc.GetByBusinessAndPlatform(ctx, uuid.New(), "google")

		assert.ErrorIs(t, err, domain.ErrIntegrationNotFound)
		assert.Nil(t, result)
	})

	t.Run("error - nil business id", func(t *testing.T) {
		repo := &mockIntegrationRepository{}
		svc := NewIntegrationService(repo, testEncryptor(t), nil)

		result, err := svc.GetByBusinessAndPlatform(ctx, uuid.Nil, "google")

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "business id is required")
	})

	t.Run("error - empty platform", func(t *testing.T) {
		repo := &mockIntegrationRepository{}
		svc := NewIntegrationService(repo, testEncryptor(t), nil)

		result, err := svc.GetByBusinessAndPlatform(ctx, uuid.New(), "")

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "platform is required")
	})

	t.Run("error - canceled context", func(t *testing.T) {
		cancelledCtx, cancel := context.WithCancel(context.Background())
		cancel()

		repo := &mockIntegrationRepository{}
		svc := NewIntegrationService(repo, testEncryptor(t), nil)

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

		svc := NewIntegrationService(repo, testEncryptor(t), nil)
		result, err := svc.GetByBusinessAndPlatform(ctx, uuid.New(), "google")

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "get integration")
	})
}

func TestIntegrationService_Delete(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		integrationID := uuid.New()
		var deletedID uuid.UUID

		repo := &mockIntegrationRepository{
			deleteFunc: func(ctx context.Context, id uuid.UUID) error {
				deletedID = id
				return nil
			},
		}

		svc := NewIntegrationService(repo, testEncryptor(t), nil)
		err := svc.Delete(ctx, integrationID)

		require.NoError(t, err)
		assert.Equal(t, integrationID, deletedID)
	})

	t.Run("integration not found", func(t *testing.T) {
		repo := &mockIntegrationRepository{
			deleteFunc: func(ctx context.Context, id uuid.UUID) error {
				return domain.ErrIntegrationNotFound
			},
		}

		svc := NewIntegrationService(repo, testEncryptor(t), nil)
		err := svc.Delete(ctx, uuid.New())

		assert.ErrorIs(t, err, domain.ErrIntegrationNotFound)
	})

	t.Run("error - nil integration id", func(t *testing.T) {
		repo := &mockIntegrationRepository{}
		svc := NewIntegrationService(repo, testEncryptor(t), nil)

		err := svc.Delete(ctx, uuid.Nil)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "integration id is required")
	})

	t.Run("error - canceled context", func(t *testing.T) {
		cancelledCtx, cancel := context.WithCancel(context.Background())
		cancel()

		repo := &mockIntegrationRepository{}
		svc := NewIntegrationService(repo, testEncryptor(t), nil)

		err := svc.Delete(cancelledCtx, uuid.New())

		assert.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("error - repository error", func(t *testing.T) {
		repoErr := errors.New("database error")
		repo := &mockIntegrationRepository{
			deleteFunc: func(ctx context.Context, id uuid.UUID) error {
				return repoErr
			},
		}

		svc := NewIntegrationService(repo, testEncryptor(t), nil)
		err := svc.Delete(ctx, uuid.New())

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "delete integration")
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

	svc := NewIntegrationService(repo, enc, nil)
	params := ConnectParams{
		BusinessID:  businessID,
		Platform:    "telegram",
		ExternalID:  "ext_telegram_123",
		AccessToken: plaintext,
	}
	result, err := svc.Connect(ctx, params)

	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify the token stored in repo is encrypted (not plaintext)
	require.NotNil(t, capturedIntegration)
	assert.NotEqual(t, []byte(plaintext), capturedIntegration.EncryptedAccessToken)
	assert.NotEmpty(t, capturedIntegration.EncryptedAccessToken)

	// Verify we can decrypt it back to the original plaintext
	decrypted, err := enc.Decrypt(capturedIntegration.EncryptedAccessToken)
	require.NoError(t, err)
	assert.Equal(t, plaintext, string(decrypted))

	// Verify returned integration fields
	assert.Equal(t, "telegram", result.Platform)
	assert.Equal(t, "ext_telegram_123", result.ExternalID)
	assert.Equal(t, "active", result.Status)
}

func TestConnect_Duplicate(t *testing.T) {
	ctx := context.Background()
	enc := testEncryptor(t)

	repo := &mockIntegrationRepository{
		createFunc: func(ctx context.Context, integration *domain.Integration) error {
			return domain.ErrIntegrationExists
		},
	}

	svc := NewIntegrationService(repo, enc, nil)
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
	svc := NewIntegrationService(repo, enc, nil)
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
	svc := NewIntegrationService(repo, enc, nil)
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

	// Encrypt the token as it would be stored
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

	svc := NewIntegrationService(repo, enc, nil)
	resp, err := svc.GetDecryptedToken(ctx, businessID, platform, externalID)

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

	svc := NewIntegrationService(repo, enc, nil)
	resp, err := svc.GetDecryptedToken(ctx, uuid.New(), "telegram", "ext_999")

	assert.Nil(t, resp)
	assert.ErrorIs(t, err, domain.ErrIntegrationNotFound)
}

func TestGetDecryptedToken_Expired(t *testing.T) {
	ctx := context.Background()
	enc := testEncryptor(t)

	businessID := uuid.New()
	platform := "vk"
	externalID := "vk_user_expired"

	// Token expired in the past, no refresh token
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

	svc := NewIntegrationService(repo, enc, nil)
	resp, err := svc.GetDecryptedToken(ctx, businessID, platform, externalID)

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

	svc := NewIntegrationService(repo, enc, nil)
	result, err := svc.ListByBusinessAndPlatform(ctx, businessID, platform)

	require.NoError(t, err)
	assert.Len(t, result, 2)
	assert.Equal(t, "chan_1", result[0].ExternalID)
	assert.Equal(t, "chan_2", result[1].ExternalID)
}

func TestListByBusinessAndPlatform_NilBusinessID(t *testing.T) {
	ctx := context.Background()
	enc := testEncryptor(t)

	repo := &mockIntegrationRepository{}
	svc := NewIntegrationService(repo, enc, nil)

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
			// Return the same expired integration for double-check after lock
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

	svc := NewIntegrationService(repo, enc, refresher)
	resp, err := svc.GetDecryptedToken(ctx, businessID, platform, externalID)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, newAccess, resp.AccessToken)
	assert.Equal(t, 1, refresher.callCount)

	// Verify tokens were persisted
	require.NotNil(t, updatedIntegration)
	decAccess, err := enc.Decrypt(updatedIntegration.EncryptedAccessToken)
	require.NoError(t, err)
	assert.Equal(t, newAccess, string(decAccess))

	// Verify expiry was updated
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

	svc := NewIntegrationService(repo, enc, refresher)
	resp, err := svc.GetDecryptedToken(ctx, businessID, "google_business", "locations/99")

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, newAccess, resp.AccessToken)

	// Verify rotated refresh token was persisted
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

	// nil refresher
	svc := NewIntegrationService(repo, enc, nil)
	resp, err := svc.GetDecryptedToken(ctx, businessID, "google_business", "loc/1")

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
		EncryptedRefreshToken: nil, // no refresh token
		TokenExpiresAt:        &past,
	}

	repo := &mockIntegrationRepository{
		getByBusinessPlatformExternalFunc: func(ctx context.Context, bid uuid.UUID, plat string, extID string) (*domain.Integration, error) {
			return integration, nil
		},
	}

	refresher := &mockTokenRefresher{}
	svc := NewIntegrationService(repo, enc, refresher)
	resp, err := svc.GetDecryptedToken(ctx, businessID, "google_business", "loc/1")

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
	svc := NewIntegrationService(repo, enc, refresher)
	resp, err := svc.GetDecryptedToken(ctx, businessID, "google_business", "loc/2")

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

	// After first refresh, return updated integration from DB
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
				// Second concurrent call sees already-refreshed token
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

	svc := NewIntegrationService(repo, enc, refresher)

	// Make two sequential calls (serialized by mutex)
	resp1, err := svc.GetDecryptedToken(ctx, businessID, "google_business", "loc/1")
	require.NoError(t, err)
	assert.Equal(t, newAccess, resp1.AccessToken)

	resp2, err := svc.GetDecryptedToken(ctx, businessID, "google_business", "loc/1")
	require.NoError(t, err)
	assert.Equal(t, newAccess, resp2.AccessToken)

	// Only one actual refresh should have happened (second call sees fresh token after re-read)
	assert.Equal(t, 1, refresher.callCount, "should only refresh once due to mutex + double-check")
}
