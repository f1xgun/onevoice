package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/auth"
)

// Mock UserRepository
type mockUserRepository struct {
	createFunc                func(ctx context.Context, user *domain.User) error
	getByIDFunc               func(ctx context.Context, id uuid.UUID) (*domain.User, error)
	getByEmailFunc            func(ctx context.Context, email string) (*domain.User, error)
	updateFunc                func(ctx context.Context, user *domain.User) error
	updatePreferredLocaleFunc func(ctx context.Context, userID uuid.UUID, locale string) error
	updateNameFunc            func(ctx context.Context, userID uuid.UUID, name string) error
}

func (m *mockUserRepository) Create(ctx context.Context, user *domain.User) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, user)
	}
	return nil
}

// GetByID mirrors the real repo's `deleted_at IS NULL` filter: a soft-deleted
// row returned by getByIDFunc surfaces as ErrUserNotFound, so the deletion-aware
// code paths are genuinely exercised (and revert-proofed) against the mock.
func (m *mockUserRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	if m.getByIDFunc != nil {
		u, err := m.getByIDFunc(ctx, id)
		if err == nil && u != nil && u.DeletedAt != nil {
			return nil, domain.ErrUserNotFound
		}
		return u, err
	}
	return nil, domain.ErrUserNotFound
}

// GetByIDIncludingDeleted satisfies the widened domain.UserRepository
// interface and, unlike GetByID, returns soft-deleted rows verbatim.
func (m *mockUserRepository) GetByIDIncludingDeleted(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	if m.getByIDFunc != nil {
		return m.getByIDFunc(ctx, id)
	}
	return nil, domain.ErrUserNotFound
}

// GetByEmail mirrors the real repo's `deleted_at IS NULL` filter: a soft-deleted
// row returned by getByEmailFunc surfaces as ErrUserNotFound.
func (m *mockUserRepository) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	if m.getByEmailFunc != nil {
		u, err := m.getByEmailFunc(ctx, email)
		if err == nil && u != nil && u.DeletedAt != nil {
			return nil, domain.ErrUserNotFound
		}
		return u, err
	}
	return nil, domain.ErrUserNotFound
}

// GetByEmailIncludingDeleted satisfies the widened domain.UserRepository
// interface and, unlike GetByEmail, returns soft-deleted rows verbatim.
func (m *mockUserRepository) GetByEmailIncludingDeleted(ctx context.Context, email string) (*domain.User, error) {
	if m.getByEmailFunc != nil {
		return m.getByEmailFunc(ctx, email)
	}
	return nil, domain.ErrUserNotFound
}

func (m *mockUserRepository) Update(ctx context.Context, user *domain.User) error {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, user)
	}
	return nil
}

func (m *mockUserRepository) UpdatePreferredLocale(ctx context.Context, userID uuid.UUID, locale string) error {
	if m.updatePreferredLocaleFunc != nil {
		return m.updatePreferredLocaleFunc(ctx, userID, locale)
	}
	return nil
}

func (m *mockUserRepository) UpdateName(ctx context.Context, userID uuid.UUID, name string) error {
	if m.updateNameFunc != nil {
		return m.updateNameFunc(ctx, userID, name)
	}
	return nil
}

// Test helpers
func setupRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.NewMiniRedis()
	require.NoError(t, mr.Start())

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	t.Cleanup(func() {
		_ = client.Close()
		mr.Close()
	})

	return client, mr
}

func TestUserService_UpdateName(t *testing.T) {
	ctx := context.Background()
	redisClient, _ := setupRedis(t)
	jwtSecret := "test-secret-must-be-32bytes-ok!!"

	t.Run("trims whitespace and persists", func(t *testing.T) {
		var got string
		repo := &mockUserRepository{
			updateNameFunc: func(_ context.Context, _ uuid.UUID, name string) error {
				got = name
				return nil
			},
		}
		svc, _ := NewUserService(repo, redisClient, jwtSecret)
		require.NoError(t, svc.UpdateName(ctx, uuid.New(), "  Jane Doe  "))
		assert.Equal(t, "Jane Doe", got)
	})

	t.Run("propagates ErrUserNotFound", func(t *testing.T) {
		repo := &mockUserRepository{
			updateNameFunc: func(_ context.Context, _ uuid.UUID, _ string) error {
				return domain.ErrUserNotFound
			},
		}
		svc, _ := NewUserService(repo, redisClient, jwtSecret)
		err := svc.UpdateName(ctx, uuid.New(), "Jane")
		assert.ErrorIs(t, err, domain.ErrUserNotFound)
	})
}

func TestUserService_Register(t *testing.T) {
	ctx := context.Background()
	redisClient, _ := setupRedis(t)
	jwtSecret := "test-secret-must-be-32bytes-ok!!"

	t.Run("success", func(t *testing.T) {
		var createdUser *domain.User
		repo := &mockUserRepository{
			createFunc: func(ctx context.Context, user *domain.User) error {
				createdUser = user
				return nil
			},
		}

		svc, _ := NewUserService(repo, redisClient, jwtSecret)
		user, err := svc.Register(ctx, "test@example.com", "Zx9!mK7-qP2w")

		require.NoError(t, err)
		assert.NotNil(t, user)
		assert.Equal(t, "test@example.com", user.Email)
		assert.Empty(t, user.PasswordHash, "password hash should be sanitized")
		assert.NotEqual(t, uuid.Nil, user.ID)

		assert.NotNil(t, createdUser)
		assert.NotEmpty(t, createdUser.PasswordHash)
		assert.NotEqual(t, "Zx9!mK7-qP2w", createdUser.PasswordHash)
		err = bcrypt.CompareHashAndPassword([]byte(createdUser.PasswordHash), []byte("Zx9!mK7-qP2w"))
		assert.NoError(t, err, "password should be correctly hashed")
	})

	t.Run("invalid email - empty", func(t *testing.T) {
		repo := &mockUserRepository{}
		svc, _ := NewUserService(repo, redisClient, jwtSecret)

		user, err := svc.Register(ctx, "", "password123")

		assert.Error(t, err)
		assert.Nil(t, user)
		assert.Contains(t, err.Error(), "invalid email")
	})

	t.Run("invalid email - no @", func(t *testing.T) {
		repo := &mockUserRepository{}
		svc, _ := NewUserService(repo, redisClient, jwtSecret)

		user, err := svc.Register(ctx, "notanemail", "password123")

		assert.Error(t, err)
		assert.Nil(t, user)
		assert.Contains(t, err.Error(), "invalid email")
	})

	t.Run("invalid email - too short", func(t *testing.T) {
		repo := &mockUserRepository{}
		svc, _ := NewUserService(repo, redisClient, jwtSecret)

		user, err := svc.Register(ctx, "a@", "password123")

		assert.Error(t, err)
		assert.Nil(t, user)
		assert.Contains(t, err.Error(), "invalid email")
	})

	t.Run("empty password", func(t *testing.T) {
		repo := &mockUserRepository{}
		svc, _ := NewUserService(repo, redisClient, jwtSecret)

		user, err := svc.Register(ctx, "test@example.com", "")

		assert.Error(t, err)
		assert.Nil(t, user)
		assert.Contains(t, err.Error(), "password")
	})

	t.Run("user already exists", func(t *testing.T) {
		repo := &mockUserRepository{
			createFunc: func(ctx context.Context, user *domain.User) error {
				return domain.ErrUserExists
			},
		}

		svc, _ := NewUserService(repo, redisClient, jwtSecret)
		user, err := svc.Register(ctx, "test@example.com", "Zx9!mK7-qP2w")

		assert.ErrorIs(t, err, domain.ErrUserExists)
		assert.Nil(t, user)
	})

	t.Run("repository error", func(t *testing.T) {
		repoErr := errors.New("database connection failed")
		repo := &mockUserRepository{
			createFunc: func(ctx context.Context, user *domain.User) error {
				return repoErr
			},
		}

		svc, _ := NewUserService(repo, redisClient, jwtSecret)
		user, err := svc.Register(ctx, "test@example.com", "Zx9!mK7-qP2w")

		assert.Error(t, err)
		assert.Nil(t, user)
		assert.Contains(t, err.Error(), "create user")
	})
}

func TestUserService_Login(t *testing.T) {
	ctx := context.Background()
	redisClient, mr := setupRedis(t)
	jwtSecret := "test-secret-must-be-32bytes-ok!!"

	t.Run("success", func(t *testing.T) {
		passwordHash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
		require.NoError(t, err)

		existingUser := &domain.User{
			ID:           uuid.New(),
			Email:        "test@example.com",
			PasswordHash: string(passwordHash),
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}

		repo := &mockUserRepository{
			getByEmailFunc: func(ctx context.Context, email string) (*domain.User, error) {
				if email == existingUser.Email {
					return existingUser, nil
				}
				return nil, domain.ErrUserNotFound
			},
		}

		svc, _ := NewUserService(repo, redisClient, jwtSecret)
		user, accessToken, refreshToken, err := svc.Login(ctx, "test@example.com", "password123")

		require.NoError(t, err)
		assert.NotNil(t, user)
		assert.Equal(t, existingUser.ID, user.ID)
		assert.Equal(t, existingUser.Email, user.Email)
		assert.Empty(t, user.PasswordHash, "password hash should be sanitized")
		assert.NotEmpty(t, accessToken)
		assert.NotEmpty(t, refreshToken)

		token, err := jwt.ParseWithClaims(accessToken, &auth.AccessTokenClaims{}, func(token *jwt.Token) (interface{}, error) {
			return []byte(jwtSecret), nil
		})
		require.NoError(t, err)
		assert.True(t, token.Valid)

		claims, ok := token.Claims.(*auth.AccessTokenClaims)
		require.True(t, ok)
		assert.Equal(t, existingUser.ID, claims.UserID)
		assert.Equal(t, existingUser.Email, claims.Email)

		refreshTokenParsed, err := jwt.ParseWithClaims(refreshToken, &auth.RefreshTokenClaims{}, func(token *jwt.Token) (interface{}, error) {
			return []byte(jwtSecret), nil
		})
		require.NoError(t, err)
		assert.True(t, refreshTokenParsed.Valid)

		refreshClaims, ok := refreshTokenParsed.Claims.(*auth.RefreshTokenClaims)
		require.True(t, ok)
		assert.Equal(t, existingUser.ID, refreshClaims.UserID)
		assert.NotEqual(t, uuid.Nil, refreshClaims.TokenID)

		val, err := redisClient.Get(ctx, "onevoice:auth:refresh_token:"+refreshClaims.TokenID.String()).Result()
		require.NoError(t, err)
		assert.Equal(t, existingUser.ID.String(), val)

		ttl, err := redisClient.TTL(ctx, "onevoice:auth:refresh_token:"+refreshClaims.TokenID.String()).Result()
		require.NoError(t, err)
		assert.Greater(t, ttl.Seconds(), float64(604700))
		assert.Less(t, ttl.Seconds(), float64(604900))
	})

	t.Run("user not found", func(t *testing.T) {
		repo := &mockUserRepository{
			getByEmailFunc: func(ctx context.Context, email string) (*domain.User, error) {
				return nil, domain.ErrUserNotFound
			},
		}

		svc, _ := NewUserService(repo, redisClient, jwtSecret)
		user, accessToken, refreshToken, err := svc.Login(ctx, "nonexistent@example.com", "password123")

		assert.ErrorIs(t, err, domain.ErrInvalidCredentials)
		assert.Nil(t, user)
		assert.Empty(t, accessToken)
		assert.Empty(t, refreshToken)
	})

	t.Run("invalid password", func(t *testing.T) {
		passwordHash, err := bcrypt.GenerateFromPassword([]byte("correctpassword"), bcrypt.DefaultCost)
		require.NoError(t, err)

		existingUser := &domain.User{
			ID:           uuid.New(),
			Email:        "test@example.com",
			PasswordHash: string(passwordHash),
		}

		repo := &mockUserRepository{
			getByEmailFunc: func(ctx context.Context, email string) (*domain.User, error) {
				return existingUser, nil
			},
		}

		svc, _ := NewUserService(repo, redisClient, jwtSecret)
		user, accessToken, refreshToken, err := svc.Login(ctx, "test@example.com", "wrongpassword")

		assert.ErrorIs(t, err, domain.ErrInvalidCredentials)
		assert.Nil(t, user)
		assert.Empty(t, accessToken)
		assert.Empty(t, refreshToken)
	})

	t.Run("repository error", func(t *testing.T) {
		repoErr := errors.New("database error")
		repo := &mockUserRepository{
			getByEmailFunc: func(ctx context.Context, email string) (*domain.User, error) {
				return nil, repoErr
			},
		}

		svc, _ := NewUserService(repo, redisClient, jwtSecret)
		user, accessToken, refreshToken, err := svc.Login(ctx, "test@example.com", "password123")

		assert.Error(t, err)
		assert.Nil(t, user)
		assert.Empty(t, accessToken)
		assert.Empty(t, refreshToken)
		assert.Contains(t, err.Error(), "get user")
	})

	t.Run("redis error", func(t *testing.T) {
		mr.Close()

		passwordHash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
		require.NoError(t, err)

		existingUser := &domain.User{
			ID:           uuid.New(),
			Email:        "test@example.com",
			PasswordHash: string(passwordHash),
		}

		repo := &mockUserRepository{
			getByEmailFunc: func(ctx context.Context, email string) (*domain.User, error) {
				return existingUser, nil
			},
		}

		svc, _ := NewUserService(repo, redisClient, jwtSecret)
		user, accessToken, refreshToken, err := svc.Login(ctx, "test@example.com", "password123")

		assert.Error(t, err)
		assert.Nil(t, user)
		assert.Empty(t, accessToken)
		assert.Empty(t, refreshToken)
		assert.Contains(t, err.Error(), "store refresh token")
	})
}

func TestUserService_RefreshToken(t *testing.T) {
	ctx := context.Background()
	redisClient, mr := setupRedis(t)
	jwtSecret := "test-secret-must-be-32bytes-ok!!"

	t.Run("success", func(t *testing.T) {
		userID := uuid.New()
		existingUser := &domain.User{
			ID:    userID,
			Email: "test@example.com",
		}

		repo := &mockUserRepository{
			getByIDFunc: func(ctx context.Context, id uuid.UUID) (*domain.User, error) {
				if id == userID {
					return existingUser, nil
				}
				return nil, domain.ErrUserNotFound
			},
		}

		svc, _ := NewUserService(repo, redisClient, jwtSecret)

		tokenID := uuid.New()
		refreshClaims := &auth.RefreshTokenClaims{
			UserID:  userID,
			TokenID: tokenID,
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    auth.TokenIssuer,
				Audience:  jwt.ClaimStrings{auth.TokenAudience},
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(7 * 24 * time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
			},
		}

		refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
		refreshTokenString, err := refreshToken.SignedString([]byte(jwtSecret))
		require.NoError(t, err)

		err = redisClient.Set(ctx, "onevoice:auth:refresh_token:"+tokenID.String(), userID.String(), 7*24*time.Hour).Err()
		require.NoError(t, err)

		user, newAccessToken, newRefreshToken, err := svc.RefreshToken(ctx, refreshTokenString)

		require.NoError(t, err)
		assert.NotNil(t, user)
		assert.Equal(t, userID, user.ID)
		assert.Equal(t, existingUser.Email, user.Email)
		assert.Empty(t, user.PasswordHash, "password hash should be sanitized")
		assert.NotEmpty(t, newAccessToken)
		assert.NotEmpty(t, newRefreshToken)

		_, err = redisClient.Get(ctx, "onevoice:auth:refresh_token:"+tokenID.String()).Result()
		assert.ErrorIs(t, err, redis.Nil)

		token, err := jwt.ParseWithClaims(newAccessToken, &auth.AccessTokenClaims{}, func(token *jwt.Token) (interface{}, error) {
			return []byte(jwtSecret), nil
		})
		require.NoError(t, err)
		assert.True(t, token.Valid)

		claims, ok := token.Claims.(*auth.AccessTokenClaims)
		require.True(t, ok)
		assert.Equal(t, userID, claims.UserID)
		assert.Equal(t, existingUser.Email, claims.Email)

		newToken, err := jwt.ParseWithClaims(newRefreshToken, &auth.RefreshTokenClaims{}, func(token *jwt.Token) (interface{}, error) {
			return []byte(jwtSecret), nil
		})
		require.NoError(t, err)
		newClaims, ok := newToken.Claims.(*auth.RefreshTokenClaims)
		require.True(t, ok)
		assert.Equal(t, userID, newClaims.UserID)
		assert.NotEqual(t, tokenID, newClaims.TokenID, "new token should have different ID")

		val, err := redisClient.Get(ctx, "onevoice:auth:refresh_token:"+newClaims.TokenID.String()).Result()
		require.NoError(t, err)
		assert.Equal(t, userID.String(), val)
	})

	t.Run("invalid token format", func(t *testing.T) {
		repo := &mockUserRepository{}
		svc, _ := NewUserService(repo, redisClient, jwtSecret)

		user, accessToken, refreshToken, err := svc.RefreshToken(ctx, "invalid-token")

		assert.ErrorIs(t, err, domain.ErrInvalidToken)
		assert.Nil(t, user)
		assert.Empty(t, accessToken)
		assert.Empty(t, refreshToken)
	})

	t.Run("expired token", func(t *testing.T) {
		_ = uuid.New()
		tokenID := uuid.New()

		repo := &mockUserRepository{}
		svc, _ := NewUserService(repo, redisClient, jwtSecret)

		refreshClaims := &auth.RefreshTokenClaims{
			TokenID: tokenID,
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    auth.TokenIssuer,
				Audience:  jwt.ClaimStrings{auth.TokenAudience},
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
			},
		}

		refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
		refreshTokenString, err := refreshToken.SignedString([]byte(jwtSecret))
		require.NoError(t, err)

		user, accessToken, newRefresh, err := svc.RefreshToken(ctx, refreshTokenString)

		assert.ErrorIs(t, err, domain.ErrInvalidToken)
		assert.Nil(t, user)
		assert.Empty(t, accessToken)
		assert.Empty(t, newRefresh)
	})

	t.Run("token not in redis", func(t *testing.T) {
		_ = uuid.New()
		tokenID := uuid.New()

		repo := &mockUserRepository{}
		svc, _ := NewUserService(repo, redisClient, jwtSecret)

		refreshClaims := &auth.RefreshTokenClaims{
			TokenID: tokenID,
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    auth.TokenIssuer,
				Audience:  jwt.ClaimStrings{auth.TokenAudience},
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(7 * 24 * time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
			},
		}

		refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
		refreshTokenString, err := refreshToken.SignedString([]byte(jwtSecret))
		require.NoError(t, err)

		user, accessToken, newRefresh, err := svc.RefreshToken(ctx, refreshTokenString)

		assert.ErrorIs(t, err, domain.ErrInvalidToken)
		assert.Nil(t, user)
		assert.Empty(t, accessToken)
		assert.Empty(t, newRefresh)
	})

	t.Run("user not found", func(t *testing.T) {
		userID := uuid.New()
		tokenID := uuid.New()

		repo := &mockUserRepository{
			getByIDFunc: func(ctx context.Context, id uuid.UUID) (*domain.User, error) {
				return nil, domain.ErrUserNotFound
			},
		}

		svc, _ := NewUserService(repo, redisClient, jwtSecret)

		refreshClaims := &auth.RefreshTokenClaims{
			UserID:  userID,
			TokenID: tokenID,
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    auth.TokenIssuer,
				Audience:  jwt.ClaimStrings{auth.TokenAudience},
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(7 * 24 * time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
			},
		}

		refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
		refreshTokenString, err := refreshToken.SignedString([]byte(jwtSecret))
		require.NoError(t, err)

		err = redisClient.Set(ctx, "onevoice:auth:refresh_token:"+tokenID.String(), userID.String(), 7*24*time.Hour).Err()
		require.NoError(t, err)

		user, accessToken, newRefresh, err := svc.RefreshToken(ctx, refreshTokenString)

		assert.ErrorIs(t, err, domain.ErrUserNotFound)
		assert.Nil(t, user)
		assert.Empty(t, accessToken)
		assert.Empty(t, newRefresh)
	})

	t.Run("redis error", func(t *testing.T) {
		_ = uuid.New()
		tokenID := uuid.New()

		repo := &mockUserRepository{}
		svc, _ := NewUserService(repo, redisClient, jwtSecret)

		refreshClaims := &auth.RefreshTokenClaims{
			TokenID: tokenID,
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    auth.TokenIssuer,
				Audience:  jwt.ClaimStrings{auth.TokenAudience},
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(7 * 24 * time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
			},
		}

		refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
		refreshTokenString, err := refreshToken.SignedString([]byte(jwtSecret))
		require.NoError(t, err)

		mr.Close()

		user, accessToken, newRefresh, err := svc.RefreshToken(ctx, refreshTokenString)

		assert.Error(t, err)
		assert.Nil(t, user)
		assert.Empty(t, accessToken)
		assert.Empty(t, newRefresh)
		assert.Contains(t, err.Error(), "validate refresh token")
	})
}

func TestUserService_Logout(t *testing.T) {
	ctx := context.Background()
	jwtSecret := "test-secret-must-be-32bytes-ok!!"

	t.Run("success", func(t *testing.T) {
		redisClient, _ := setupRedis(t)
		repo := &mockUserRepository{}
		svc, _ := NewUserService(repo, redisClient, jwtSecret)

		tokenID := uuid.New()
		userID := uuid.New()

		err := redisClient.Set(ctx, "onevoice:auth:refresh_token:"+tokenID.String(), userID.String(), 7*24*time.Hour).Err()
		require.NoError(t, err)

		refreshClaims := &auth.RefreshTokenClaims{
			TokenID: tokenID,
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    auth.TokenIssuer,
				Audience:  jwt.ClaimStrings{auth.TokenAudience},
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(7 * 24 * time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
			},
		}

		refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
		refreshTokenString, err := refreshToken.SignedString([]byte(jwtSecret))
		require.NoError(t, err)

		err = svc.Logout(ctx, refreshTokenString)

		require.NoError(t, err)

		_, err = redisClient.Get(ctx, "onevoice:auth:refresh_token:"+tokenID.String()).Result()
		assert.ErrorIs(t, err, redis.Nil)
	})

	t.Run("invalid token format", func(t *testing.T) {
		redisClient, _ := setupRedis(t)
		repo := &mockUserRepository{}
		svc, _ := NewUserService(repo, redisClient, jwtSecret)

		err := svc.Logout(ctx, "invalid-token")

		assert.ErrorIs(t, err, domain.ErrInvalidToken)
	})

	t.Run("expired token", func(t *testing.T) {
		redisClient, _ := setupRedis(t)
		repo := &mockUserRepository{}
		svc, _ := NewUserService(repo, redisClient, jwtSecret)

		_ = uuid.New()
		tokenID := uuid.New()

		refreshClaims := &auth.RefreshTokenClaims{
			TokenID: tokenID,
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    auth.TokenIssuer,
				Audience:  jwt.ClaimStrings{auth.TokenAudience},
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
			},
		}

		refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
		refreshTokenString, err := refreshToken.SignedString([]byte(jwtSecret))
		require.NoError(t, err)

		err = svc.Logout(ctx, refreshTokenString)

		assert.ErrorIs(t, err, domain.ErrInvalidToken)
	})

	t.Run("redis error", func(t *testing.T) {
		redisClient, mr := setupRedis(t)
		repo := &mockUserRepository{}
		svc, _ := NewUserService(repo, redisClient, jwtSecret)

		_ = uuid.New()
		tokenID := uuid.New()

		refreshClaims := &auth.RefreshTokenClaims{
			TokenID: tokenID,
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    auth.TokenIssuer,
				Audience:  jwt.ClaimStrings{auth.TokenAudience},
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(7 * 24 * time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
			},
		}

		refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
		refreshTokenString, err := refreshToken.SignedString([]byte(jwtSecret))
		require.NoError(t, err)

		mr.Close()

		err = svc.Logout(ctx, refreshTokenString)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "delete refresh token")
	})

	t.Run("token not in redis - no error", func(t *testing.T) {
		redisClient, _ := setupRedis(t)
		repo := &mockUserRepository{}
		svc, _ := NewUserService(repo, redisClient, jwtSecret)

		_ = uuid.New()
		tokenID := uuid.New()

		refreshClaims := &auth.RefreshTokenClaims{
			TokenID: tokenID,
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    auth.TokenIssuer,
				Audience:  jwt.ClaimStrings{auth.TokenAudience},
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(7 * 24 * time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
			},
		}

		refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
		refreshTokenString, err := refreshToken.SignedString([]byte(jwtSecret))
		require.NoError(t, err)

		err = svc.Logout(ctx, refreshTokenString)

		require.NoError(t, err)
	})
}

func TestUserService_GetByID(t *testing.T) {
	ctx := context.Background()
	redisClient, _ := setupRedis(t)
	jwtSecret := "test-secret-must-be-32bytes-ok!!"

	t.Run("success", func(t *testing.T) {
		userID := uuid.New()
		existingUser := &domain.User{
			ID:           userID,
			Email:        "test@example.com",
			PasswordHash: "hashed-password",
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}

		repo := &mockUserRepository{
			getByIDFunc: func(ctx context.Context, id uuid.UUID) (*domain.User, error) {
				if id == userID {
					return existingUser, nil
				}
				return nil, domain.ErrUserNotFound
			},
		}

		svc, _ := NewUserService(repo, redisClient, jwtSecret)
		user, err := svc.GetByID(ctx, userID)

		require.NoError(t, err)
		assert.NotNil(t, user)
		assert.Equal(t, userID, user.ID)
		assert.Equal(t, "test@example.com", user.Email)
		assert.Empty(t, user.PasswordHash, "password hash should be sanitized")
	})

	t.Run("user not found", func(t *testing.T) {
		repo := &mockUserRepository{
			getByIDFunc: func(ctx context.Context, id uuid.UUID) (*domain.User, error) {
				return nil, domain.ErrUserNotFound
			},
		}

		svc, _ := NewUserService(repo, redisClient, jwtSecret)
		user, err := svc.GetByID(ctx, uuid.New())

		assert.ErrorIs(t, err, domain.ErrUserNotFound)
		assert.Nil(t, user)
	})

	t.Run("repository error", func(t *testing.T) {
		repoErr := errors.New("database error")
		repo := &mockUserRepository{
			getByIDFunc: func(ctx context.Context, id uuid.UUID) (*domain.User, error) {
				return nil, repoErr
			},
		}

		svc, _ := NewUserService(repo, redisClient, jwtSecret)
		user, err := svc.GetByID(ctx, uuid.New())

		assert.Error(t, err)
		assert.Nil(t, user)
		assert.Contains(t, err.Error(), "get user")
	})
}

// TestUserService_UpdatePreferredLocale verifies the service layer delegates
// straight to the repo, propagates ErrUserNotFound unwrapped (so the handler
// can errors.Is against it for the 404 branch), and wraps unknown errors.
func TestUserService_UpdatePreferredLocale(t *testing.T) {
	ctx := context.Background()
	redisClient, _ := setupRedis(t)
	jwtSecret := "test-secret-must-be-32bytes-ok!!"

	t.Run("success", func(t *testing.T) {
		userID := uuid.New()
		var calledWith struct {
			id     uuid.UUID
			locale string
		}

		repo := &mockUserRepository{
			updatePreferredLocaleFunc: func(_ context.Context, id uuid.UUID, locale string) error {
				calledWith.id = id
				calledWith.locale = locale
				return nil
			},
		}

		svc, _ := NewUserService(repo, redisClient, jwtSecret)
		err := svc.UpdatePreferredLocale(ctx, userID, "en")

		require.NoError(t, err)
		assert.Equal(t, userID, calledWith.id)
		assert.Equal(t, "en", calledWith.locale)
	})

	t.Run("user not found - error propagated unwrapped", func(t *testing.T) {
		repo := &mockUserRepository{
			updatePreferredLocaleFunc: func(_ context.Context, _ uuid.UUID, _ string) error {
				return domain.ErrUserNotFound
			},
		}

		svc, _ := NewUserService(repo, redisClient, jwtSecret)
		err := svc.UpdatePreferredLocale(ctx, uuid.New(), "ru")

		assert.ErrorIs(t, err, domain.ErrUserNotFound)
	})

	t.Run("generic repo error - wrapped", func(t *testing.T) {
		repoErr := errors.New("postgres write failed")
		repo := &mockUserRepository{
			updatePreferredLocaleFunc: func(_ context.Context, _ uuid.UUID, _ string) error {
				return repoErr
			},
		}

		svc, _ := NewUserService(repo, redisClient, jwtSecret)
		err := svc.UpdatePreferredLocale(ctx, uuid.New(), "ru")

		assert.Error(t, err)
		assert.NotErrorIs(t, err, domain.ErrUserNotFound)
		assert.Contains(t, err.Error(), "update preferred locale")
	})
}

func TestSanitizeUser(t *testing.T) {
	user := &domain.User{
		ID:           uuid.New(),
		Email:        "test@example.com",
		PasswordHash: "secret-hash",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	sanitized := sanitizeUser(user)

	assert.NotNil(t, sanitized)
	assert.Equal(t, user.ID, sanitized.ID)
	assert.Equal(t, user.Email, sanitized.Email)
	assert.Empty(t, sanitized.PasswordHash)

	assert.NotEmpty(t, user.PasswordHash)
}

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		name    string
		email   string
		wantErr bool
	}{
		{"valid email", "test@example.com", false},
		{"valid email with plus", "test+tag@example.com", false},
		{"valid email with subdomain", "user@mail.example.com", false},
		{"empty email", "", true},
		{"no @ symbol", "notanemail", true},
		{"too short", "a@", true},
		{"only @", "@", true},
		{"missing domain", "test@", true},
		{"missing local", "@example.com", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEmail(tt.email)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGenerateTokens(t *testing.T) {
	jwtSecret := "test-secret-must-be-32bytes-ok!!"
	user := &domain.User{
		ID:    uuid.New(),
		Email: "test@example.com",
	}

	t.Run("generate access token", func(t *testing.T) {
		token, err := generateAccessToken(user, []byte(jwtSecret))
		require.NoError(t, err)
		assert.NotEmpty(t, token)

		parsed, err := jwt.ParseWithClaims(token, &auth.AccessTokenClaims{}, func(token *jwt.Token) (interface{}, error) {
			return []byte(jwtSecret), nil
		})
		require.NoError(t, err)
		assert.True(t, parsed.Valid)

		claims, ok := parsed.Claims.(*auth.AccessTokenClaims)
		require.True(t, ok)
		assert.Equal(t, user.ID, claims.UserID)
		assert.Equal(t, user.Email, claims.Email)
	})

	t.Run("generate refresh token", func(t *testing.T) {
		token, tokenID, err := generateRefreshToken(user.ID, []byte(jwtSecret))
		require.NoError(t, err)
		assert.NotEmpty(t, token)
		assert.NotEqual(t, uuid.Nil, tokenID)

		parsed, err := jwt.ParseWithClaims(token, &auth.RefreshTokenClaims{}, func(token *jwt.Token) (interface{}, error) {
			return []byte(jwtSecret), nil
		})
		require.NoError(t, err)
		assert.True(t, parsed.Valid)

		claims, ok := parsed.Claims.(*auth.RefreshTokenClaims)
		require.True(t, ok)
		assert.Equal(t, user.ID, claims.UserID)
		assert.Equal(t, tokenID, claims.TokenID)
	})
}

// --- Register tx-flow ---------------------------------------

// fakeRegisterTx is a minimal pgx.Tx double for tests that exercise the
// Register tx-flow without bringing up Postgres. Only Begin / Commit /
// Rollback semantics are tracked; queries are stubbed by the fake repos.
type fakeRegisterPool struct {
	beginErr  error
	commitErr error
	beginCnt  int
	commitCnt int
	rollCnt   int
}

func (f *fakeRegisterPool) Begin(_ context.Context) (pgx.Tx, error) {
	f.beginCnt++
	if f.beginErr != nil {
		return nil, f.beginErr
	}
	return &fakeRegisterTx{owner: f}, nil
}

type fakeRegisterTx struct {
	owner *fakeRegisterPool
	pgx.Tx
}

func (t *fakeRegisterTx) Commit(_ context.Context) error {
	t.owner.commitCnt++
	return t.owner.commitErr
}
func (t *fakeRegisterTx) Rollback(_ context.Context) error {
	t.owner.rollCnt++
	return nil
}

type fakeRegisterUserExt struct {
	createCnt int
	createErr error
	lastUser  *domain.User
}

func (f *fakeRegisterUserExt) CreateInTx(_ context.Context, _ pgx.Tx, u *domain.User) error {
	f.createCnt++
	f.lastUser = u
	return f.createErr
}

type fakeConsentInserter struct {
	insertCnt   int
	lastPurpose string
	lastPolicy  string
	insertErr   error
}

func (f *fakeConsentInserter) Insert(_ context.Context, _ pgx.Tx, _ uuid.UUID, purpose, policy string) error {
	f.insertCnt++
	f.lastPurpose = purpose
	f.lastPolicy = policy
	return f.insertErr
}

type fakeVerifyIssuer struct {
	issueCnt int
	issueErr error
	lastUID  uuid.UUID
	lastMail string
}

func (f *fakeVerifyIssuer) IssueAndEnqueueTx(_ context.Context, _ pgx.Tx, uid uuid.UUID, mail string) error {
	f.issueCnt++
	f.lastUID = uid
	f.lastMail = mail
	return f.issueErr
}

func TestUserService_Register_TxFlow_AtomicSuccess(t *testing.T) {
	ctx := context.Background()
	redisClient, _ := setupRedis(t)
	repo := &mockUserRepository{}

	svc, err := NewUserService(repo, redisClient, "test-secret-must-be-32bytes-ok!!")
	require.NoError(t, err)

	pool := &fakeRegisterPool{}
	userExt := &fakeRegisterUserExt{}
	consents := &fakeConsentInserter{}
	verify := &fakeVerifyIssuer{}

	svc.(*userService).SetRegisterCollaborators(pool, userExt, consents, verify, nil)

	user, err := svc.Register(ctx, "alice@example.com", "Zx9!mK7-qP2w")
	require.NoError(t, err)
	require.NotNil(t, user)
	require.Equal(t, "alice@example.com", user.Email)

	require.Equal(t, 1, pool.beginCnt)
	require.Equal(t, 1, pool.commitCnt)

	require.Equal(t, 1, userExt.createCnt)
	require.Equal(t, 1, consents.insertCnt)
	require.Equal(t, 1, verify.issueCnt)

	require.Equal(t, "service_operation", consents.lastPurpose)
	require.Equal(t, "pre-v22", consents.lastPolicy)

	require.Equal(t, "alice@example.com", verify.lastMail)
	require.Equal(t, user.ID, verify.lastUID)
}

func TestUserService_Register_TxFlow_VerifyFailureRollsBack(t *testing.T) {
	ctx := context.Background()
	redisClient, _ := setupRedis(t)
	repo := &mockUserRepository{}

	svc, err := NewUserService(repo, redisClient, "test-secret-must-be-32bytes-ok!!")
	require.NoError(t, err)

	pool := &fakeRegisterPool{}
	userExt := &fakeRegisterUserExt{}
	consents := &fakeConsentInserter{}
	verify := &fakeVerifyIssuer{issueErr: assertSentinel("outbox enqueue blew up")}

	svc.(*userService).SetRegisterCollaborators(pool, userExt, consents, verify, nil)

	user, err := svc.Register(ctx, "alice@example.com", "Zx9!mK7-qP2w")
	require.Error(t, err)
	require.Nil(t, user)

	require.Equal(t, 1, pool.beginCnt)
	require.Equal(t, 0, pool.commitCnt, "verify failure must short-circuit before commit")
	require.GreaterOrEqual(t, pool.rollCnt, 1)
}

// assertSentinel is a tiny helper to produce an error value the test can
// inspect without importing extra packages.
type assertSentinel string

func (e assertSentinel) Error() string { return string(e) }

func TestValidatePassword(t *testing.T) {
	t.Run("weak dictionary password is too weak", func(t *testing.T) {
		require.ErrorIs(t, validatePassword("password123"), ErrPasswordTooWeak)
	})

	t.Run("too short is too weak", func(t *testing.T) {
		require.ErrorIs(t, validatePassword("Ab1!"), ErrPasswordTooWeak)
	})

	t.Run("strong password passes", func(t *testing.T) {
		require.NoError(t, validatePassword("Zx9!mK7-qP2w"))
	})
}

// signRefreshToken mints a signed refresh token and seeds its Redis binding so
// RefreshToken's GetDel rotation finds it. Mirrors the inline setup the older
// RefreshToken cases repeat.
func signRefreshToken(t *testing.T, ctx context.Context, rdb *redis.Client, jwtSecret string, userID uuid.UUID) string {
	t.Helper()
	tokenID := uuid.New()
	claims := &auth.RefreshTokenClaims{
		UserID:  userID,
		TokenID: tokenID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    auth.TokenIssuer,
			Audience:  jwt.ClaimStrings{auth.TokenAudience},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(7 * 24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(jwtSecret))
	require.NoError(t, err)
	require.NoError(t, rdb.Set(ctx, refreshTokenKeyPrefix+tokenID.String(), userID.String(), 7*24*time.Hour).Err())
	return signed
}

// TestUserService_DeletionAwareTokenMinting is the Bug A regression: a user who
// requested deletion (deleted_at set) but is still inside the restore grace
// window must be able to mint a fresh access token via RefreshToken AND log in,
// so the auth-gated restore endpoint stays reachable. A user PAST the grace
// boundary (purge-eligible) must still be rejected.
//
// Fail-on-revert: switching RefreshToken back to GetByID (or Login back to
// GetByEmail) makes the within-grace sub-tests fail with ErrUserNotFound /
// ErrInvalidCredentials, because those getters filter `deleted_at IS NULL`.
func TestUserService_DeletionAwareTokenMinting(t *testing.T) {
	ctx := context.Background()
	jwtSecret := "test-secret-must-be-32bytes-ok!!"

	withinGrace := time.Now().Add(-time.Duration(deletionGraceDays-1) * 24 * time.Hour)
	pastGrace := time.Now().Add(-time.Duration(deletionGraceDays+1) * 24 * time.Hour)
	deletedNow := time.Now()

	newSoftDeleted := func(requestedAt time.Time) *domain.User {
		passwordHash, err := bcrypt.GenerateFromPassword([]byte("Zx9!mK7-qP2w"), bcrypt.DefaultCost)
		require.NoError(t, err)
		return &domain.User{
			ID:                  uuid.New(),
			Email:               "pending@example.com",
			PasswordHash:        string(passwordHash),
			DeletedAt:           &deletedNow,
			DeletionRequestedAt: &requestedAt,
		}
	}

	t.Run("RefreshToken within grace mints fresh token", func(t *testing.T) {
		redisClient, _ := setupRedis(t)
		user := newSoftDeleted(withinGrace)
		repo := &mockUserRepository{
			getByIDFunc: func(_ context.Context, id uuid.UUID) (*domain.User, error) {
				require.Equal(t, user.ID, id)
				return user, nil
			},
		}
		svc, _ := NewUserService(repo, redisClient, jwtSecret)

		refreshToken := signRefreshToken(t, ctx, redisClient, jwtSecret, user.ID)
		got, accessToken, newRefresh, err := svc.RefreshToken(ctx, refreshToken)

		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, user.ID, got.ID)
		assert.NotEmpty(t, accessToken)
		assert.NotEmpty(t, newRefresh)
	})

	t.Run("RefreshToken past grace is rejected", func(t *testing.T) {
		redisClient, _ := setupRedis(t)
		user := newSoftDeleted(pastGrace)
		repo := &mockUserRepository{
			getByIDFunc: func(_ context.Context, _ uuid.UUID) (*domain.User, error) {
				return user, nil
			},
		}
		svc, _ := NewUserService(repo, redisClient, jwtSecret)

		refreshToken := signRefreshToken(t, ctx, redisClient, jwtSecret, user.ID)
		got, accessToken, newRefresh, err := svc.RefreshToken(ctx, refreshToken)

		assert.ErrorIs(t, err, domain.ErrUserNotFound)
		assert.Nil(t, got)
		assert.Empty(t, accessToken)
		assert.Empty(t, newRefresh)
	})

	t.Run("Login within grace authenticates so restore is reachable", func(t *testing.T) {
		redisClient, _ := setupRedis(t)
		user := newSoftDeleted(withinGrace)
		repo := &mockUserRepository{
			getByEmailFunc: func(_ context.Context, email string) (*domain.User, error) {
				require.Equal(t, user.Email, email)
				return user, nil
			},
		}
		svc, _ := NewUserService(repo, redisClient, jwtSecret)

		got, accessToken, refreshToken, err := svc.Login(ctx, user.Email, "Zx9!mK7-qP2w")

		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, user.ID, got.ID)
		assert.NotEmpty(t, accessToken)
		assert.NotEmpty(t, refreshToken)
	})

	t.Run("Login past grace is rejected as invalid credentials", func(t *testing.T) {
		redisClient, _ := setupRedis(t)
		user := newSoftDeleted(pastGrace)
		repo := &mockUserRepository{
			getByEmailFunc: func(_ context.Context, _ string) (*domain.User, error) {
				return user, nil
			},
		}
		svc, _ := NewUserService(repo, redisClient, jwtSecret)

		got, accessToken, refreshToken, err := svc.Login(ctx, user.Email, "Zx9!mK7-qP2w")

		assert.ErrorIs(t, err, domain.ErrInvalidCredentials)
		assert.Nil(t, got)
		assert.Empty(t, accessToken)
		assert.Empty(t, refreshToken)
	})
}

// TestUserService_ChangePassword_WipesRefreshTokens is the Bug B regression:
// a successful password change must invalidate every outstanding refresh token
// for that user (mirroring the password-reset flow), so an attacker-held token
// cannot keep minting access tokens up to its 7-day TTL. The other user's
// tokens must survive.
//
// Fail-on-revert: removing the wipeRefreshTokensForUser call from ChangePassword
// leaves the seeded keys in Redis and this test fails.
func TestUserService_ChangePassword_WipesRefreshTokens(t *testing.T) {
	ctx := context.Background()
	jwtSecret := "test-secret-must-be-32bytes-ok!!"
	redisClient, _ := setupRedis(t)

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("Zx9!mK7-qP2w"), bcrypt.DefaultCost)
	require.NoError(t, err)

	userID := uuid.New()
	otherID := uuid.New()
	user := &domain.User{ID: userID, Email: "user@example.com", PasswordHash: string(passwordHash)}

	repo := &mockUserRepository{
		getByIDFunc: func(_ context.Context, id uuid.UUID) (*domain.User, error) {
			if id == userID {
				return user, nil
			}
			return nil, domain.ErrUserNotFound
		},
		updateFunc: func(_ context.Context, _ *domain.User) error { return nil },
	}
	svc, _ := NewUserService(repo, redisClient, jwtSecret)

	for i := 0; i < 3; i++ {
		require.NoError(t, redisClient.Set(ctx,
			fmt.Sprintf("%st-%d", refreshTokenKeyPrefix, i), userID.String(), time.Hour).Err())
	}
	require.NoError(t, redisClient.Set(ctx,
		refreshTokenKeyPrefix+"o-0", otherID.String(), time.Hour).Err())

	require.NoError(t, svc.ChangePassword(ctx, userID, "Zx9!mK7-qP2w", "Qw8#vR2-nL5t"))

	for i := 0; i < 3; i++ {
		_, gerr := redisClient.Get(ctx, fmt.Sprintf("%st-%d", refreshTokenKeyPrefix, i)).Result()
		require.ErrorIs(t, gerr, redis.Nil, "changed user's refresh tokens must be wiped")
	}
	val, gerr := redisClient.Get(ctx, refreshTokenKeyPrefix+"o-0").Result()
	require.NoError(t, gerr)
	require.Equal(t, otherID.String(), val, "other user's refresh token must survive")
}
