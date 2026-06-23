package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"github.com/trustelem/zxcvbn"
	"golang.org/x/crypto/bcrypt"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/auth"
)

// JWT token expiry durations.
const (
	AccessTokenExpiry     = 15 * time.Minute
	RefreshTokenExpiry    = 7 * 24 * time.Hour
	refreshTokenKeyPrefix = "onevoice:auth:refresh_token:" //nolint:gosec // not a credential, just a Redis key prefix
)

// User credential validation thresholds. See docs/services/user.md.
const (
	// 72 is the bcrypt silent-truncation boundary; 8 is the conventional lower bound.
	passwordMinLen = 8
	passwordMaxLen = 72
	// passwordMinZxcvbnScore is the floor for the zxcvbn strength estimate
	// (0..4). 2 = "somewhat guessable, protection from throttled online
	// attack" — adequate given login lockout/captcha, while still rejecting
	// top-dictionary and keyboard-walk passwords.
	passwordMinZxcvbnScore = 2
)

// RegistrationContext carries the per-request context needed by the atomic-Register flow.
// See docs/services/user.md.
type RegistrationContext struct {
	IP        string
	UserAgent string
	// Name is the display name from the registration form. Persisted on the
	// atomic register path; trimmed by RegisterWithContext.
	Name     string
	Policies []PolicyAccepted
}

// UserService defines the interface for user-related operations.
// See docs/services/user.md.
type UserService interface {
	Register(ctx context.Context, email, password string) (*domain.User, error)
	// RegisterWithContext is the atomic-Register entry point used by the handler.
	RegisterWithContext(ctx context.Context, email, password string, regCtx RegistrationContext) (*domain.User, error)
	Login(ctx context.Context, email, password string) (user *domain.User, accessToken, refreshToken string, err error)
	RefreshToken(ctx context.Context, refreshToken string) (user *domain.User, accessToken, newRefreshToken string, err error)
	Logout(ctx context.Context, refreshToken string) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error)
	ChangePassword(ctx context.Context, userID uuid.UUID, currentPassword, newPassword string) error
	// UpdatePreferredLocale persists the user's UI language choice.
	UpdatePreferredLocale(ctx context.Context, userID uuid.UUID, locale string) error
	// UpdateName persists the user's display name (PATCH /auth/profile).
	UpdateName(ctx context.Context, userID uuid.UUID, name string) error
}

type userService struct {
	repo      domain.UserRepository
	redis     *redis.Client
	jwtSecret []byte

	// Register collaborators are optional; nil disables the atomic path. See docs/services/user.md.
	registerPool     RegisterTxPool
	registerUserRepo RegisterUserExt
	registerConsents ConsentInserter
	registerVerify   RegisterVerifyIssuer
	registerAudit    audit.Logger

	registerConsentSvc *ConsentService
}

// RegisterTxPool is the tx-opening seam needed by Register.
type RegisterTxPool interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// RegisterUserExt is the tx-aware user insert seam.
type RegisterUserExt interface {
	CreateInTx(ctx context.Context, tx pgx.Tx, user *domain.User) error
}

// ConsentInserter records the initial 'service_operation' consent row inside the same tx.
type ConsentInserter interface {
	Insert(ctx context.Context, tx pgx.Tx, userID uuid.UUID, purpose, policyVersion string) error
}

// RegisterVerifyIssuer is the EmailVerificationService.IssueAndEnqueueTx seam.
type RegisterVerifyIssuer interface {
	IssueAndEnqueueTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, email string) error
}

// SetRegisterCollaborators wires the Register tx-flow deps. See docs/services/user.md.
func (s *userService) SetRegisterCollaborators(
	pool RegisterTxPool,
	userRepo RegisterUserExt,
	consents ConsentInserter,
	verify RegisterVerifyIssuer,
	auditLogger audit.Logger,
) {
	s.registerPool = pool
	s.registerUserRepo = userRepo
	s.registerConsents = consents
	s.registerVerify = verify
	s.registerAudit = auditLogger
}

// SetRegisterConsentService wires the multi-consent (tos/privacy/pdn) tx writer. See docs/services/user.md.
func (s *userService) SetRegisterConsentService(consentSvc *ConsentService) {
	s.registerConsentSvc = consentSvc
}

// Compile-time check that userService implements UserService
var _ UserService = (*userService)(nil)

// NewUserService creates a new user service instance
func NewUserService(repo domain.UserRepository, redisClient *redis.Client, jwtSecret string) (UserService, error) {
	if len(jwtSecret) < auth.JWTSecretMinLen {
		return nil, fmt.Errorf("NewUserService: jwt secret must be at least %d bytes (got %d)", auth.JWTSecretMinLen, len(jwtSecret))
	}
	return &userService{
		repo:      repo,
		redis:     redisClient,
		jwtSecret: []byte(jwtSecret),
	}, nil
}

// Register creates a new user with encrypted password. See docs/services/user.md.
func (s *userService) Register(ctx context.Context, email, password string) (*domain.User, error) {
	if err := validateEmail(email); err != nil {
		return nil, err
	}

	if err := validatePassword(password); err != nil {
		return nil, err
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	user := &domain.User{
		ID:           uuid.New(),
		Email:        email,
		PasswordHash: string(passwordHash),
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if s.registerPool != nil && s.registerUserRepo != nil && s.registerConsents != nil && s.registerVerify != nil {
		tx, err := s.registerPool.Begin(ctx)
		if err != nil {
			return nil, fmt.Errorf("register begin tx: %w", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()

		if err := s.registerUserRepo.CreateInTx(ctx, tx, user); err != nil {
			if errors.Is(err, domain.ErrUserExists) {
				return nil, err
			}
			return nil, fmt.Errorf("register create user tx: %w", err)
		}
		if err := s.registerConsents.Insert(ctx, tx, user.ID, "service_operation", "pre-v22"); err != nil {
			return nil, fmt.Errorf("register insert consent: %w", err)
		}
		if err := s.registerVerify.IssueAndEnqueueTx(ctx, tx, user.ID, user.Email); err != nil {
			return nil, fmt.Errorf("register issue verify: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("register commit: %w", err)
		}

		if s.registerAudit != nil {
			audit.LogConsentRecorded(ctx, s.registerAudit, user.ID, "service_operation", "pre-v22")
		}
		return sanitizeUser(user), nil
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

// RegisterWithContext is the atomic-Register entry point. See docs/services/user.md.
func (s *userService) RegisterWithContext(ctx context.Context, email, password string, regCtx RegistrationContext) (*domain.User, error) {
	if err := validateEmail(email); err != nil {
		return nil, err
	}
	if err := validatePassword(password); err != nil {
		return nil, err
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	user := &domain.User{
		ID:           uuid.New(),
		Email:        email,
		Name:         strings.TrimSpace(regCtx.Name),
		PasswordHash: string(passwordHash),
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if s.registerPool != nil && s.registerUserRepo != nil && s.registerVerify != nil && s.registerConsentSvc != nil {
		tx, err := s.registerPool.Begin(ctx)
		if err != nil {
			return nil, fmt.Errorf("register begin tx: %w", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()

		if err := s.registerUserRepo.CreateInTx(ctx, tx, user); err != nil {
			if errors.Is(err, domain.ErrUserExists) {
				return nil, err
			}
			return nil, fmt.Errorf("register create user tx: %w", err)
		}
		if err := s.registerConsentSvc.RecordRegistrationConsents(ctx, tx, user.ID, regCtx.IP, regCtx.UserAgent, regCtx.Policies); err != nil {
			return nil, fmt.Errorf("record registration consents: %w", err)
		}
		if err := s.registerVerify.IssueAndEnqueueTx(ctx, tx, user.ID, user.Email); err != nil {
			return nil, fmt.Errorf("register issue verify: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("register commit: %w", err)
		}
		return sanitizeUser(user), nil
	}

	return s.Register(ctx, email, password)
}

// Login authenticates user and issues access and refresh tokens
func (s *userService) Login(ctx context.Context, email, password string) (user *domain.User, accessToken, refreshToken string, err error) {
	user, err = s.repo.GetByEmail(ctx, email)
	if err != nil && !errors.Is(err, domain.ErrUserNotFound) {
		return nil, "", "", fmt.Errorf("get user: %w", err)
	}

	dummyHash := "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
	hashToCompare := dummyHash
	if user != nil {
		hashToCompare = user.PasswordHash
	}

	err = bcrypt.CompareHashAndPassword([]byte(hashToCompare), []byte(password))
	if err != nil || user == nil {
		return nil, "", "", domain.ErrInvalidCredentials
	}

	accessToken, err = generateAccessToken(user, s.jwtSecret)
	if err != nil {
		return nil, "", "", fmt.Errorf("generate access token: %w", err)
	}

	var tokenID uuid.UUID
	refreshToken, tokenID, err = generateRefreshToken(user.ID, s.jwtSecret)
	if err != nil {
		return nil, "", "", fmt.Errorf("generate refresh token: %w", err)
	}

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
	token, err := jwt.ParseWithClaims(refreshToken, &auth.RefreshTokenClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.jwtSecret, nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(auth.TokenIssuer), jwt.WithAudience(auth.TokenAudience))

	if err != nil {
		return nil, "", "", domain.ErrInvalidToken
	}

	claims, ok := token.Claims.(*auth.RefreshTokenClaims)
	if !ok || !token.Valid {
		return nil, "", "", domain.ErrInvalidToken
	}

	oldKey := refreshTokenKeyPrefix + claims.TokenID.String()
	userID, err := s.redis.GetDel(ctx, oldKey).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, "", "", domain.ErrInvalidToken
		}
		return nil, "", "", fmt.Errorf("validate refresh token: %w", err)
	}

	if userID != claims.UserID.String() {
		return nil, "", "", domain.ErrInvalidToken
	}

	user, err = s.repo.GetByID(ctx, claims.UserID)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return nil, "", "", err
		}
		return nil, "", "", fmt.Errorf("get user: %w", err)
	}

	accessToken, err = generateAccessToken(user, s.jwtSecret)
	if err != nil {
		return nil, "", "", fmt.Errorf("generate access token: %w", err)
	}

	var newTokenID uuid.UUID
	newRefreshToken, newTokenID, err = generateRefreshToken(user.ID, s.jwtSecret)
	if err != nil {
		return nil, "", "", fmt.Errorf("generate refresh token: %w", err)
	}

	newKey := refreshTokenKeyPrefix + newTokenID.String()
	err = s.redis.Set(ctx, newKey, user.ID.String(), RefreshTokenExpiry).Err()
	if err != nil {
		return nil, "", "", fmt.Errorf("store refresh token: %w", err)
	}

	return sanitizeUser(user), accessToken, newRefreshToken, nil
}

// Logout invalidates a refresh token
func (s *userService) Logout(ctx context.Context, refreshToken string) error {
	token, err := jwt.ParseWithClaims(refreshToken, &auth.RefreshTokenClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.jwtSecret, nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(auth.TokenIssuer), jwt.WithAudience(auth.TokenAudience))

	if err != nil {
		return domain.ErrInvalidToken
	}

	claims, ok := token.Claims.(*auth.RefreshTokenClaims)
	if !ok || !token.Valid {
		return domain.ErrInvalidToken
	}

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

// UpdatePreferredLocale persists the user's UI language choice. See docs/services/user.md.
func (s *userService) UpdatePreferredLocale(ctx context.Context, userID uuid.UUID, locale string) error {
	if err := s.repo.UpdatePreferredLocale(ctx, userID, locale); err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return err
		}
		return fmt.Errorf("update preferred locale: %w", err)
	}
	return nil
}

// UpdateName persists the user's display name. Length is enforced at the
// handler boundary (RegisterRequest / UpdateProfileRequest schema); the service
// trims surrounding whitespace before the write.
func (s *userService) UpdateName(ctx context.Context, userID uuid.UUID, name string) error {
	if err := s.repo.UpdateName(ctx, userID, strings.TrimSpace(name)); err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return err
		}
		return fmt.Errorf("update name: %w", err)
	}
	return nil
}

// ChangePassword validates current password and updates to new password
func (s *userService) ChangePassword(ctx context.Context, userID uuid.UUID, currentPassword, newPassword string) error {
	user, err := s.repo.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return err
		}
		return fmt.Errorf("get user: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(currentPassword)); err != nil {
		return domain.ErrInvalidCredentials
	}

	if err := validatePassword(newPassword); err != nil {
		return err
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	user.PasswordHash = string(newHash)
	user.UpdatedAt = time.Now()

	if err := s.repo.Update(ctx, user); err != nil {
		return fmt.Errorf("update user: %w", err)
	}

	return nil
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

	parts := strings.Split(email, "@")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("invalid email format")
	}

	return nil
}

// validatePassword checks if password meets security requirements: a length
// band plus a zxcvbn strength floor. A too-short password is reported as
// ErrPasswordTooWeak so the handlers map it consistently; a too-long password
// (the frontend zod schema caps at 72, so it is never hit from the UI) keeps
// its descriptive error since it is not "weak".
func validatePassword(password string) error {
	if len(password) < passwordMinLen {
		return ErrPasswordTooWeak
	}
	if len(password) > passwordMaxLen {
		return fmt.Errorf("password must be at most %d characters", passwordMaxLen)
	}
	if zxcvbn.PasswordStrength(password, nil).Score < passwordMinZxcvbnScore {
		return ErrPasswordTooWeak
	}
	return nil
}

// generateAccessToken creates a new JWT access token
func generateAccessToken(user *domain.User, secret []byte) (string, error) {
	claims := &auth.AccessTokenClaims{
		UserID: user.ID,
		Email:  user.Email,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    auth.TokenIssuer,
			Audience:  jwt.ClaimStrings{auth.TokenAudience},
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

	claims := &auth.RefreshTokenClaims{
		UserID:  userID,
		TokenID: tokenID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    auth.TokenIssuer,
			Audience:  jwt.ClaimStrings{auth.TokenAudience},
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
