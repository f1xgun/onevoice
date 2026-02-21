package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// Mock UserRepository
type mockUserRepository struct {
	createFunc     func(ctx context.Context, user *domain.User) error
	getByIDFunc    func(ctx context.Context, id uuid.UUID) (*domain.User, error)
	getByEmailFunc func(ctx context.Context, email string) (*domain.User, error)
	updateFunc     func(ctx context.Context, user *domain.User) error
}

func (m *mockUserRepository) Create(ctx context.Context, user *domain.User) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, user)
	}
	return nil
}

func (m *mockUserRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	if m.getByIDFunc != nil {
		return m.getByIDFunc(ctx, id)
	}
	return nil, domain.ErrUserNotFound
}

func (m *mockUserRepository) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
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

		svc := NewUserService(repo, redisClient, jwtSecret)
		user, err := svc.Register(ctx, "test@example.com", "password123")

		require.NoError(t, err)
		assert.NotNil(t, user)
		assert.Equal(t, "test@example.com", user.Email)
		assert.Empty(t, user.PasswordHash, "password hash should be sanitized")
		assert.NotEqual(t, uuid.Nil, user.ID)

		// Verify password was hashed
		assert.NotNil(t, createdUser)
		assert.NotEmpty(t, createdUser.PasswordHash)
		assert.NotEqual(t, "password123", createdUser.PasswordHash)
		err = bcrypt.CompareHashAndPassword([]byte(createdUser.PasswordHash), []byte("password123"))
		assert.NoError(t, err, "password should be correctly hashed")
	})

	t.Run("invalid email - empty", func(t *testing.T) {
		repo := &mockUserRepository{}
		svc := NewUserService(repo, redisClient, jwtSecret)

		user, err := svc.Register(ctx, "", "password123")

		assert.Error(t, err)
		assert.Nil(t, user)
		assert.Contains(t, err.Error(), "invalid email")
	})

	t.Run("invalid email - no @", func(t *testing.T) {
		repo := &mockUserRepository{}
		svc := NewUserService(repo, redisClient, jwtSecret)

		user, err := svc.Register(ctx, "notanemail", "password123")

		assert.Error(t, err)
		assert.Nil(t, user)
		assert.Contains(t, err.Error(), "invalid email")
	})

	t.Run("invalid email - too short", func(t *testing.T) {
		repo := &mockUserRepository{}
		svc := NewUserService(repo, redisClient, jwtSecret)

		user, err := svc.Register(ctx, "a@", "password123")

		assert.Error(t, err)
		assert.Nil(t, user)
		assert.Contains(t, err.Error(), "invalid email")
	})

	t.Run("empty password", func(t *testing.T) {
		repo := &mockUserRepository{}
		svc := NewUserService(repo, redisClient, jwtSecret)

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

		svc := NewUserService(repo, redisClient, jwtSecret)
		user, err := svc.Register(ctx, "test@example.com", "password123")

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

		svc := NewUserService(repo, redisClient, jwtSecret)
		user, err := svc.Register(ctx, "test@example.com", "password123")

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
		// Prepare user with hashed password
		passwordHash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
		require.NoError(t, err)

		existingUser := &domain.User{
			ID:           uuid.New(),
			Email:        "test@example.com",
			PasswordHash: string(passwordHash),
			Role:         domain.RoleOwner,
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

		svc := NewUserService(repo, redisClient, jwtSecret)
		user, accessToken, refreshToken, err := svc.Login(ctx, "test@example.com", "password123")

		require.NoError(t, err)
		assert.NotNil(t, user)
		assert.Equal(t, existingUser.ID, user.ID)
		assert.Equal(t, existingUser.Email, user.Email)
		assert.Empty(t, user.PasswordHash, "password hash should be sanitized")
		assert.NotEmpty(t, accessToken)
		assert.NotEmpty(t, refreshToken)

		// Verify access token
		token, err := jwt.ParseWithClaims(accessToken, &AccessTokenClaims{}, func(token *jwt.Token) (interface{}, error) {
			return []byte(jwtSecret), nil
		})
		require.NoError(t, err)
		assert.True(t, token.Valid)

		claims, ok := token.Claims.(*AccessTokenClaims)
		require.True(t, ok)
		assert.Equal(t, existingUser.ID, claims.UserID)
		assert.Equal(t, existingUser.Email, claims.Email)
		assert.Equal(t, string(existingUser.Role), claims.Role)

		// Verify refresh token
		refreshTokenParsed, err := jwt.ParseWithClaims(refreshToken, &RefreshTokenClaims{}, func(token *jwt.Token) (interface{}, error) {
			return []byte(jwtSecret), nil
		})
		require.NoError(t, err)
		assert.True(t, refreshTokenParsed.Valid)

		refreshClaims, ok := refreshTokenParsed.Claims.(*RefreshTokenClaims)
		require.True(t, ok)
		assert.Equal(t, existingUser.ID, refreshClaims.UserID)
		assert.NotEqual(t, uuid.Nil, refreshClaims.TokenID)

		// Verify refresh token stored in Redis
		val, err := redisClient.Get(ctx, "onevoice:auth:refresh_token:"+refreshClaims.TokenID.String()).Result()
		require.NoError(t, err)
		assert.Equal(t, existingUser.ID.String(), val)

		// Verify TTL is approximately 7 days
		ttl, err := redisClient.TTL(ctx, "onevoice:auth:refresh_token:"+refreshClaims.TokenID.String()).Result()
		require.NoError(t, err)
		assert.Greater(t, ttl.Seconds(), float64(604700)) // ~7 days - 100s margin
		assert.Less(t, ttl.Seconds(), float64(604900))    // ~7 days + 100s margin
	})

	t.Run("user not found", func(t *testing.T) {
		repo := &mockUserRepository{
			getByEmailFunc: func(ctx context.Context, email string) (*domain.User, error) {
				return nil, domain.ErrUserNotFound
			},
		}

		svc := NewUserService(repo, redisClient, jwtSecret)
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
			Role:         domain.RoleOwner,
		}

		repo := &mockUserRepository{
			getByEmailFunc: func(ctx context.Context, email string) (*domain.User, error) {
				return existingUser, nil
			},
		}

		svc := NewUserService(repo, redisClient, jwtSecret)
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

		svc := NewUserService(repo, redisClient, jwtSecret)
		user, accessToken, refreshToken, err := svc.Login(ctx, "test@example.com", "password123")

		assert.Error(t, err)
		assert.Nil(t, user)
		assert.Empty(t, accessToken)
		assert.Empty(t, refreshToken)
		assert.Contains(t, err.Error(), "get user")
	})

	t.Run("redis error", func(t *testing.T) {
		// Close miniredis to simulate Redis failure
		mr.Close()

		passwordHash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
		require.NoError(t, err)

		existingUser := &domain.User{
			ID:           uuid.New(),
			Email:        "test@example.com",
			PasswordHash: string(passwordHash),
			Role:         domain.RoleOwner,
		}

		repo := &mockUserRepository{
			getByEmailFunc: func(ctx context.Context, email string) (*domain.User, error) {
				return existingUser, nil
			},
		}

		svc := NewUserService(repo, redisClient, jwtSecret)
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
			Role:  domain.RoleOwner,
		}

		repo := &mockUserRepository{
			getByIDFunc: func(ctx context.Context, id uuid.UUID) (*domain.User, error) {
				if id == userID {
					return existingUser, nil
				}
				return nil, domain.ErrUserNotFound
			},
		}

		svc := NewUserService(repo, redisClient, jwtSecret)

		// Generate a valid refresh token
		tokenID := uuid.New()
		refreshClaims := &RefreshTokenClaims{
			UserID:  userID,
			TokenID: tokenID,
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(7 * 24 * time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
			},
		}

		refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
		refreshTokenString, err := refreshToken.SignedString([]byte(jwtSecret))
		require.NoError(t, err)

		// Store in Redis
		err = redisClient.Set(ctx, "onevoice:auth:refresh_token:"+tokenID.String(), userID.String(), 7*24*time.Hour).Err()
		require.NoError(t, err)

		// Call RefreshToken
		newAccessToken, err := svc.RefreshToken(ctx, refreshTokenString)

		require.NoError(t, err)
		assert.NotEmpty(t, newAccessToken)

		// Verify new access token
		token, err := jwt.ParseWithClaims(newAccessToken, &AccessTokenClaims{}, func(token *jwt.Token) (interface{}, error) {
			return []byte(jwtSecret), nil
		})
		require.NoError(t, err)
		assert.True(t, token.Valid)

		claims, ok := token.Claims.(*AccessTokenClaims)
		require.True(t, ok)
		assert.Equal(t, userID, claims.UserID)
		assert.Equal(t, existingUser.Email, claims.Email)
	})

	t.Run("invalid token format", func(t *testing.T) {
		repo := &mockUserRepository{}
		svc := NewUserService(repo, redisClient, jwtSecret)

		accessToken, err := svc.RefreshToken(ctx, "invalid-token")

		assert.ErrorIs(t, err, domain.ErrInvalidToken)
		assert.Empty(t, accessToken)
	})

	t.Run("expired token", func(t *testing.T) {
		userID := uuid.New()
		tokenID := uuid.New()

		repo := &mockUserRepository{}
		svc := NewUserService(repo, redisClient, jwtSecret)

		// Generate expired refresh token
		refreshClaims := &RefreshTokenClaims{
			UserID:  userID,
			TokenID: tokenID,
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)), // Expired
				IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
			},
		}

		refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
		refreshTokenString, err := refreshToken.SignedString([]byte(jwtSecret))
		require.NoError(t, err)

		accessToken, err := svc.RefreshToken(ctx, refreshTokenString)

		assert.ErrorIs(t, err, domain.ErrInvalidToken)
		assert.Empty(t, accessToken)
	})

	t.Run("token not in redis", func(t *testing.T) {
		userID := uuid.New()
		tokenID := uuid.New()

		repo := &mockUserRepository{}
		svc := NewUserService(repo, redisClient, jwtSecret)

		// Generate valid token but don't store in Redis
		refreshClaims := &RefreshTokenClaims{
			UserID:  userID,
			TokenID: tokenID,
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(7 * 24 * time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
			},
		}

		refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
		refreshTokenString, err := refreshToken.SignedString([]byte(jwtSecret))
		require.NoError(t, err)

		accessToken, err := svc.RefreshToken(ctx, refreshTokenString)

		assert.ErrorIs(t, err, domain.ErrInvalidToken)
		assert.Empty(t, accessToken)
	})

	t.Run("user not found", func(t *testing.T) {
		userID := uuid.New()
		tokenID := uuid.New()

		repo := &mockUserRepository{
			getByIDFunc: func(ctx context.Context, id uuid.UUID) (*domain.User, error) {
				return nil, domain.ErrUserNotFound
			},
		}

		svc := NewUserService(repo, redisClient, jwtSecret)

		// Generate valid token and store in Redis
		refreshClaims := &RefreshTokenClaims{
			UserID:  userID,
			TokenID: tokenID,
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(7 * 24 * time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
			},
		}

		refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
		refreshTokenString, err := refreshToken.SignedString([]byte(jwtSecret))
		require.NoError(t, err)

		err = redisClient.Set(ctx, "onevoice:auth:refresh_token:"+tokenID.String(), userID.String(), 7*24*time.Hour).Err()
		require.NoError(t, err)

		accessToken, err := svc.RefreshToken(ctx, refreshTokenString)

		assert.ErrorIs(t, err, domain.ErrUserNotFound)
		assert.Empty(t, accessToken)
	})

	t.Run("redis error", func(t *testing.T) {
		userID := uuid.New()
		tokenID := uuid.New()

		repo := &mockUserRepository{}
		svc := NewUserService(repo, redisClient, jwtSecret)

		// Generate valid token
		refreshClaims := &RefreshTokenClaims{
			UserID:  userID,
			TokenID: tokenID,
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(7 * 24 * time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
			},
		}

		refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
		refreshTokenString, err := refreshToken.SignedString([]byte(jwtSecret))
		require.NoError(t, err)

		// Close Redis to simulate error
		mr.Close()

		accessToken, err := svc.RefreshToken(ctx, refreshTokenString)

		assert.Error(t, err)
		assert.Empty(t, accessToken)
		assert.Contains(t, err.Error(), "validate refresh token")
	})
}

func TestUserService_Logout(t *testing.T) {
	ctx := context.Background()
	jwtSecret := "test-secret-must-be-32bytes-ok!!"

	t.Run("success", func(t *testing.T) {
		redisClient, _ := setupRedis(t)
		repo := &mockUserRepository{}
		svc := NewUserService(repo, redisClient, jwtSecret)

		tokenID := uuid.New()
		userID := uuid.New()

		// Store refresh token in Redis
		err := redisClient.Set(ctx, "onevoice:auth:refresh_token:"+tokenID.String(), userID.String(), 7*24*time.Hour).Err()
		require.NoError(t, err)

		// Generate refresh token
		refreshClaims := &RefreshTokenClaims{
			UserID:  userID,
			TokenID: tokenID,
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(7 * 24 * time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
			},
		}

		refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
		refreshTokenString, err := refreshToken.SignedString([]byte(jwtSecret))
		require.NoError(t, err)

		// Logout
		err = svc.Logout(ctx, refreshTokenString)

		require.NoError(t, err)

		// Verify token removed from Redis
		_, err = redisClient.Get(ctx, "onevoice:auth:refresh_token:"+tokenID.String()).Result()
		assert.ErrorIs(t, err, redis.Nil)
	})

	t.Run("invalid token format", func(t *testing.T) {
		redisClient, _ := setupRedis(t)
		repo := &mockUserRepository{}
		svc := NewUserService(repo, redisClient, jwtSecret)

		err := svc.Logout(ctx, "invalid-token")

		assert.ErrorIs(t, err, domain.ErrInvalidToken)
	})

	t.Run("expired token", func(t *testing.T) {
		redisClient, _ := setupRedis(t)
		repo := &mockUserRepository{}
		svc := NewUserService(repo, redisClient, jwtSecret)

		userID := uuid.New()
		tokenID := uuid.New()

		// Generate expired token
		refreshClaims := &RefreshTokenClaims{
			UserID:  userID,
			TokenID: tokenID,
			RegisteredClaims: jwt.RegisteredClaims{
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
		svc := NewUserService(repo, redisClient, jwtSecret)

		userID := uuid.New()
		tokenID := uuid.New()

		refreshClaims := &RefreshTokenClaims{
			UserID:  userID,
			TokenID: tokenID,
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(7 * 24 * time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
			},
		}

		refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
		refreshTokenString, err := refreshToken.SignedString([]byte(jwtSecret))
		require.NoError(t, err)

		// Close Redis to simulate connection error
		mr.Close()

		err = svc.Logout(ctx, refreshTokenString)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "delete refresh token")
	})

	t.Run("token not in redis - no error", func(t *testing.T) {
		redisClient, _ := setupRedis(t)
		repo := &mockUserRepository{}
		svc := NewUserService(repo, redisClient, jwtSecret)

		userID := uuid.New()
		tokenID := uuid.New()

		refreshClaims := &RefreshTokenClaims{
			UserID:  userID,
			TokenID: tokenID,
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(7 * 24 * time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
			},
		}

		refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
		refreshTokenString, err := refreshToken.SignedString([]byte(jwtSecret))
		require.NoError(t, err)

		// Don't store in Redis - should still succeed (idempotent)
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
			Role:         domain.RoleOwner,
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

		svc := NewUserService(repo, redisClient, jwtSecret)
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

		svc := NewUserService(repo, redisClient, jwtSecret)
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

		svc := NewUserService(repo, redisClient, jwtSecret)
		user, err := svc.GetByID(ctx, uuid.New())

		assert.Error(t, err)
		assert.Nil(t, user)
		assert.Contains(t, err.Error(), "get user")
	})
}

func TestSanitizeUser(t *testing.T) {
	user := &domain.User{
		ID:           uuid.New(),
		Email:        "test@example.com",
		PasswordHash: "secret-hash",
		Role:         domain.RoleOwner,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	sanitized := sanitizeUser(user)

	assert.NotNil(t, sanitized)
	assert.Equal(t, user.ID, sanitized.ID)
	assert.Equal(t, user.Email, sanitized.Email)
	assert.Empty(t, sanitized.PasswordHash)
	assert.Equal(t, user.Role, sanitized.Role)

	// Ensure original user is not modified
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
		Role:  domain.RoleOwner,
	}

	t.Run("generate access token", func(t *testing.T) {
		token, err := generateAccessToken(user, []byte(jwtSecret))
		require.NoError(t, err)
		assert.NotEmpty(t, token)

		// Parse and verify
		parsed, err := jwt.ParseWithClaims(token, &AccessTokenClaims{}, func(token *jwt.Token) (interface{}, error) {
			return []byte(jwtSecret), nil
		})
		require.NoError(t, err)
		assert.True(t, parsed.Valid)

		claims, ok := parsed.Claims.(*AccessTokenClaims)
		require.True(t, ok)
		assert.Equal(t, user.ID, claims.UserID)
		assert.Equal(t, user.Email, claims.Email)
		assert.Equal(t, string(user.Role), claims.Role)
	})

	t.Run("generate refresh token", func(t *testing.T) {
		token, tokenID, err := generateRefreshToken(user.ID, []byte(jwtSecret))
		require.NoError(t, err)
		assert.NotEmpty(t, token)
		assert.NotEqual(t, uuid.Nil, tokenID)

		// Parse and verify
		parsed, err := jwt.ParseWithClaims(token, &RefreshTokenClaims{}, func(token *jwt.Token) (interface{}, error) {
			return []byte(jwtSecret), nil
		})
		require.NoError(t, err)
		assert.True(t, parsed.Valid)

		claims, ok := parsed.Claims.(*RefreshTokenClaims)
		require.True(t, ok)
		assert.Equal(t, user.ID, claims.UserID)
		assert.Equal(t, tokenID, claims.TokenID)
	})
}
