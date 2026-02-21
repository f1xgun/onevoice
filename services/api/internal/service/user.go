package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// JWT token expiry durations
const (
	AccessTokenExpiry     = 15 * time.Minute
	RefreshTokenExpiry    = 7 * 24 * time.Hour
	refreshTokenKeyPrefix = "onevoice:auth:refresh_token:" //nolint:gosec // not a credential, just a Redis key prefix
)

// AccessTokenClaims represents JWT claims for access tokens
type AccessTokenClaims struct {
	UserID uuid.UUID `json:"user_id"`
	Email  string    `json:"email"`
	Role   string    `json:"role"`
	jwt.RegisteredClaims
}

// RefreshTokenClaims represents JWT claims for refresh tokens
type RefreshTokenClaims struct {
	UserID  uuid.UUID `json:"user_id"`
	TokenID uuid.UUID `json:"token_id"`
	jwt.RegisteredClaims
}

// UserService defines the interface for user-related operations
type UserService interface {
	Register(ctx context.Context, email, password string) (*domain.User, error)
	Login(ctx context.Context, email, password string) (user *domain.User, accessToken, refreshToken string, err error)
	RefreshToken(ctx context.Context, refreshToken string) (user *domain.User, accessToken, newRefreshToken string, err error)
	Logout(ctx context.Context, refreshToken string) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error)
}

type userService struct {
	repo      domain.UserRepository
	redis     *redis.Client
	jwtSecret []byte
}

// Compile-time check that userService implements UserService
var _ UserService = (*userService)(nil)

// NewUserService creates a new user service instance
func NewUserService(repo domain.UserRepository, redisClient *redis.Client, jwtSecret string) UserService {
	if len(jwtSecret) < 32 {
		panic("jwt secret must be at least 32 bytes")
	}
	return &userService{
		repo:      repo,
		redis:     redisClient,
		jwtSecret: []byte(jwtSecret),
	}
}

// Register creates a new user with encrypted password
func (s *userService) Register(ctx context.Context, email, password string) (*domain.User, error) {
	// Validate email
	if err := validateEmail(email); err != nil {
		return nil, err
	}

	// Validate password
	if err := validatePassword(password); err != nil {
		return nil, err
	}

	// Hash password
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	// Create user
	user := &domain.User{
		ID:           uuid.New(),
		Email:        email,
		PasswordHash: string(passwordHash),
		Role:         domain.RoleOwner, // Default role for new registrations
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	err = s.repo.Create(ctx, user)
	if err != nil {
		if errors.Is(err, domain.ErrUserExists) {
			return nil, err
		}
		return nil, fmt.Errorf("create user: %w", err)
	}

	return sanitizeUser(user), nil
}

// Login authenticates user and issues access and refresh tokens
func (s *userService) Login(ctx context.Context, email, password string) (user *domain.User, accessToken, refreshToken string, err error) {
	// Get user by email
	user, err = s.repo.GetByEmail(ctx, email)
	if err != nil && !errors.Is(err, domain.ErrUserNotFound) {
		return nil, "", "", fmt.Errorf("get user: %w", err)
	}

	// Always perform bcrypt comparison to prevent timing attacks
	// Use dummy hash if user doesn't exist to keep timing consistent
	dummyHash := "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
	hashToCompare := dummyHash
	if user != nil {
		hashToCompare = user.PasswordHash
	}

	err = bcrypt.CompareHashAndPassword([]byte(hashToCompare), []byte(password))
	if err != nil || user == nil {
		return nil, "", "", domain.ErrInvalidCredentials
	}

	// Generate access token
	accessToken, err = generateAccessToken(user, s.jwtSecret)
	if err != nil {
		return nil, "", "", fmt.Errorf("generate access token: %w", err)
	}

	// Generate refresh token
	var tokenID uuid.UUID
	refreshToken, tokenID, err = generateRefreshToken(user.ID, s.jwtSecret)
	if err != nil {
		return nil, "", "", fmt.Errorf("generate refresh token: %w", err)
	}

	// Store refresh token in Redis
	key := refreshTokenKeyPrefix + tokenID.String()
	err = s.redis.Set(ctx, key, user.ID.String(), RefreshTokenExpiry).Err()
	if err != nil {
		return nil, "", "", fmt.Errorf("store refresh token: %w", err)
	}

	return sanitizeUser(user), accessToken, refreshToken, nil
}

// RefreshToken validates a refresh token and returns a new token pair with user data.
// The old refresh token is revoked (rotation) and a new one is issued.
func (s *userService) RefreshToken(ctx context.Context, refreshToken string) (user *domain.User, accessToken, newRefreshToken string, err error) {
	// Parse and validate refresh token
	token, err := jwt.ParseWithClaims(refreshToken, &RefreshTokenClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.jwtSecret, nil
	})

	if err != nil {
		return nil, "", "", domain.ErrInvalidToken
	}

	claims, ok := token.Claims.(*RefreshTokenClaims)
	if !ok || !token.Valid {
		return nil, "", "", domain.ErrInvalidToken
	}

	// Verify token exists in Redis
	oldKey := refreshTokenKeyPrefix + claims.TokenID.String()
	userID, err := s.redis.Get(ctx, oldKey).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, "", "", domain.ErrInvalidToken
		}
		return nil, "", "", fmt.Errorf("validate refresh token: %w", err)
	}

	// Verify user ID matches
	if userID != claims.UserID.String() {
		return nil, "", "", domain.ErrInvalidToken
	}

	// Revoke old refresh token
	s.redis.Del(ctx, oldKey)

	// Get user from database
	user, err = s.repo.GetByID(ctx, claims.UserID)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return nil, "", "", err
		}
		return nil, "", "", fmt.Errorf("get user: %w", err)
	}

	// Generate new access token
	accessToken, err = generateAccessToken(user, s.jwtSecret)
	if err != nil {
		return nil, "", "", fmt.Errorf("generate access token: %w", err)
	}

	// Generate new refresh token (rotation)
	var newTokenID uuid.UUID
	newRefreshToken, newTokenID, err = generateRefreshToken(user.ID, s.jwtSecret)
	if err != nil {
		return nil, "", "", fmt.Errorf("generate refresh token: %w", err)
	}

	// Store new refresh token in Redis
	newKey := refreshTokenKeyPrefix + newTokenID.String()
	err = s.redis.Set(ctx, newKey, user.ID.String(), RefreshTokenExpiry).Err()
	if err != nil {
		return nil, "", "", fmt.Errorf("store refresh token: %w", err)
	}

	return sanitizeUser(user), accessToken, newRefreshToken, nil
}

// Logout invalidates a refresh token
func (s *userService) Logout(ctx context.Context, refreshToken string) error {
	// Parse refresh token
	token, err := jwt.ParseWithClaims(refreshToken, &RefreshTokenClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.jwtSecret, nil
	})

	if err != nil {
		return domain.ErrInvalidToken
	}

	claims, ok := token.Claims.(*RefreshTokenClaims)
	if !ok || !token.Valid {
		return domain.ErrInvalidToken
	}

	// Delete from Redis
	key := refreshTokenKeyPrefix + claims.TokenID.String()
	err = s.redis.Del(ctx, key).Err()
	if err != nil {
		return fmt.Errorf("delete refresh token: %w", err)
	}

	return nil
}

// GetByID retrieves a user by ID
func (s *userService) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	user, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("get user: %w", err)
	}

	return sanitizeUser(user), nil
}

// Helper functions

// sanitizeUser removes sensitive data from user before returning to caller
func sanitizeUser(user *domain.User) *domain.User {
	sanitized := *user
	sanitized.PasswordHash = ""
	return &sanitized
}

// validateEmail performs basic email validation
func validateEmail(email string) error {
	if len(email) < 3 || !strings.Contains(email, "@") {
		return fmt.Errorf("invalid email format")
	}

	// Check that @ is not at the beginning or end
	parts := strings.Split(email, "@")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("invalid email format")
	}

	return nil
}

// validatePassword checks if password meets security requirements
func validatePassword(password string) error {
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	if len(password) > 72 {
		// bcrypt silently truncates passwords longer than 72 bytes
		return fmt.Errorf("password must be at most 72 characters")
	}
	return nil
}

// generateAccessToken creates a new JWT access token
func generateAccessToken(user *domain.User, secret []byte) (string, error) {
	claims := &AccessTokenClaims{
		UserID: user.ID,
		Email:  user.Email,
		Role:   string(user.Role),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(AccessTokenExpiry)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(secret)
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}

	return tokenString, nil
}

// generateRefreshToken creates a new JWT refresh token
func generateRefreshToken(userID uuid.UUID, secret []byte) (string, uuid.UUID, error) {
	tokenID := uuid.New()

	claims := &RefreshTokenClaims{
		UserID:  userID,
		TokenID: tokenID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(RefreshTokenExpiry)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(secret)
	if err != nil {
		return "", uuid.Nil, fmt.Errorf("sign token: %w", err)
	}

	return tokenString, tokenID, nil
}
