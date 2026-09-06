// PasswordResetService implements a no-enumeration password recovery flow.
// See docs/services/password-reset.md.
package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/trustelem/zxcvbn"
	"golang.org/x/crypto/bcrypt"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/lockout"
	"github.com/f1xgun/onevoice/pkg/ratelimit"
	"github.com/f1xgun/onevoice/services/api/internal/repository"
)

// Tuning knobs for the reset flow. See docs/services/password-reset.md.
const (
	resetTokenTTL          = 30 * time.Minute
	resetTokenEntropyBytes = 32 // 256-bit token entropy
	// Pre-consume password check: weak password must not burn the one-shot token.
	resetMinPasswordLen  = 8
	resetRateLimitMax    = 3
	resetRateLimitWindow = time.Hour
	// Path appended to the configured PublicURL (the reverse-proxy origin) to
	// form the reset-confirm link. It is a FRONTEND route, so it must ride on
	// PublicURL, not a hardcoded host.
	resetConfirmURLPath = "/auth/password-reset/confirm"
	resetEmailSubject   = "Восстановление пароля — OneVoice"
)

// Service-level sentinels exposed for the handler error mapping.
var (
	ErrResetTokenInvalid = domain.ErrResetTokenInvalid
	ErrPasswordTooWeak   = errors.New("password too weak")
)

// UserRepoForReset is the segregated user-repo surface PasswordResetService needs.
// See docs/services/password-reset.md.
type UserRepoForReset interface {
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
	GetByID(ctx context.Context, userID uuid.UUID) (*domain.User, error)
	UpdatePasswordHashInTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, bcryptHash []byte) error
}

// PasswordResetService owns the no-enumeration password recovery flow.
// See docs/services/password-reset.md.
type PasswordResetService struct {
	pool      *pgxpool.Pool
	tokenRepo *repository.PasswordResetTokenRepository
	userRepo  UserRepoForReset
	outbox    *repository.EmailOutboxRepository
	auditLog  audit.Logger
	redis     *redis.Client
	lockout   *lockout.Lockout
	publicURL string
}

// NewPasswordResetService constructs a PasswordResetService. lockout is optional:
// when nil (no Redis), self-unlock on successful reset is silently skipped.
func NewPasswordResetService(
	pool *pgxpool.Pool,
	tokenRepo *repository.PasswordResetTokenRepository,
	userRepo UserRepoForReset,
	outbox *repository.EmailOutboxRepository,
	auditLogger audit.Logger,
	redisClient *redis.Client,
	lockoutSvc *lockout.Lockout,
	publicURL string,
) *PasswordResetService {
	return &PasswordResetService{
		pool:      pool,
		tokenRepo: tokenRepo,
		userRepo:  userRepo,
		outbox:    outbox,
		auditLog:  auditLogger,
		redis:     redisClient,
		lockout:   lockoutSvc,
		publicURL: publicURL,
	}
}

// RequestReset issues a reset token and sends the recovery email. Always returns nil (no enumeration).
// See docs/services/password-reset.md for the full lifecycle.
func (s *PasswordResetService) RequestReset(ctx context.Context, emailAddr, clientIP, userAgent string) error {
	emailAddr = domain.NormalizeEmail(emailAddr)
	rateLimited := s.bumpRateLimit(ctx, emailAddr)

	user, err := s.userRepo.GetByEmail(ctx, emailAddr)
	switch {
	case errors.Is(err, domain.ErrUserNotFound):
		s.audit(ctx, audit.ActionPasswordResetUnknownEmail, nil, map[string]any{
			"attempted_email": emailAddr,
			"ip":              clientIP,
			"user_agent":      userAgent,
		})
		return nil
	case err != nil:
		slog.ErrorContext(ctx, "password reset: get user by email", "error", err)
		s.audit(ctx, audit.ActionPasswordResetUnknownEmail, nil, map[string]any{
			"attempted_email": emailAddr,
			"ip":              clientIP,
			"user_agent":      userAgent,
			"error":           "db_error",
		})
		return nil
	}

	if rateLimited {
		s.audit(ctx, audit.ActionPasswordResetRequested, &user.ID, map[string]any{
			"ip":           clientIP,
			"user_agent":   userAgent,
			"rate_limited": true,
		})
		return nil
	}

	plaintext, hash, err := generateOpaqueToken()
	if err != nil {
		slog.ErrorContext(ctx, "password reset: rand.Read failed", "error", err)
		s.audit(ctx, audit.ActionPasswordResetRequested, &user.ID, map[string]any{
			"ip":         clientIP,
			"user_agent": userAgent,
			"error":      "rand_failed",
		})
		return nil
	}
	expiresAt := time.Now().Add(resetTokenTTL)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "password reset: tx begin", "error", err)
		return nil
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := s.tokenRepo.InvalidateAllForUser(ctx, tx, user.ID); err != nil {
		slog.ErrorContext(ctx, "password reset: invalidate prior tokens", "error", err)
		return nil
	}
	if err := s.tokenRepo.Insert(ctx, tx, user.ID, hash, expiresAt); err != nil {
		slog.ErrorContext(ctx, "password reset: insert token", "error", err)
		return nil
	}
	if _, err := s.outbox.Enqueue(ctx, tx, repository.OutboxEnqueueInput{
		ToEmail:  user.Email,
		Subject:  resetEmailSubject,
		BodyText: buildResetEmailPlainText(s.publicURL+resetConfirmURLPath, plaintext),
		BodyHTML: buildResetEmailHTML(s.publicURL+resetConfirmURLPath, plaintext),
	}); err != nil {
		slog.ErrorContext(ctx, "password reset: enqueue outbox", "error", err)
		return nil
	}
	if err := tx.Commit(ctx); err != nil {
		slog.ErrorContext(ctx, "password reset: tx commit", "error", err)
		return nil
	}

	s.audit(ctx, audit.ActionPasswordResetRequested, &user.ID, map[string]any{
		"ip":         clientIP,
		"user_agent": userAgent,
	})
	return nil
}

// ConfirmReset consumes the token and stores the new password hash.
// See docs/services/password-reset.md.
func (s *PasswordResetService) ConfirmReset(ctx context.Context, plaintextToken, newPassword, clientIP, userAgent string) error {
	if len(newPassword) < resetMinPasswordLen {
		return ErrPasswordTooWeak
	}
	if zxcvbn.PasswordStrength(newPassword, nil).Score < passwordMinZxcvbnScore {
		return ErrPasswordTooWeak
	}
	if plaintextToken == "" {
		return ErrResetTokenInvalid
	}
	hash := sha256.Sum256([]byte(plaintextToken))

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("password reset: tx begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	userID, err := s.tokenRepo.ConsumeAtomic(ctx, tx, hash[:])
	if err != nil {
		if errors.Is(err, domain.ErrResetTokenInvalid) {
			return ErrResetTokenInvalid
		}
		return fmt.Errorf("password reset: consume token: %w", err)
	}

	bcryptHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("password reset: bcrypt: %w", err)
	}
	if err := s.userRepo.UpdatePasswordHashInTx(ctx, tx, userID, bcryptHash); err != nil {
		return fmt.Errorf("password reset: update password hash: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("password reset: tx commit: %w", err)
	}

	if err := s.wipeRefreshTokens(ctx, userID); err != nil {
		slog.ErrorContext(ctx, "password reset: wipe refresh tokens", "error", err, "user_id", userID)
	}

	if s.lockout != nil {
		if u, err := s.userRepo.GetByID(ctx, userID); err == nil && u != nil {
			if err := s.lockout.ClearAllForEmail(ctx, u.Email); err != nil {
				slog.ErrorContext(ctx, "password reset: clear lockout state", "error", err, "user_id", userID)
			}
		} else if err != nil {
			slog.ErrorContext(ctx, "password reset: lookup user for lockout clear", "error", err, "user_id", userID)
		}
	}

	s.audit(ctx, audit.ActionPasswordResetCompleted, &userID, map[string]any{
		"ip":         clientIP,
		"user_agent": userAgent,
	})
	return nil
}

// bumpRateLimit returns true when the per-email counter exceeds the budget.
// Fail-open on Redis errors. See docs/services/password-reset.md.
func (s *PasswordResetService) bumpRateLimit(ctx context.Context, emailAddr string) bool {
	emailHash := sha256.Sum256([]byte(emailAddr))
	rateKey := fmt.Sprintf("reset:email:%x", emailHash)
	count, err := ratelimit.IncrWithHeal(ctx, s.redis, rateKey, resetRateLimitWindow)
	if err != nil {
		slog.ErrorContext(ctx, "password reset: rate-limit INCR failed (fail-open)", "error", err)
		return false
	}
	return count > resetRateLimitMax
}

// wipeRefreshTokens deletes every refresh-token Redis key whose value is the
// target userID, invalidating all outstanding sessions after a password reset.
// Delegates to the shared SCAN/Get/Del helper. See docs/services/password-reset.md.
func (s *PasswordResetService) wipeRefreshTokens(ctx context.Context, userID uuid.UUID) error {
	return wipeRefreshTokensForUser(ctx, s.redis, userID)
}

// audit fires a fire-and-forget audit row; details are pre-marshaled to json.RawMessage at this boundary.
func (s *PasswordResetService) audit(ctx context.Context, action string, userID *uuid.UUID, details map[string]any) {
	if s.auditLog == nil {
		return
	}
	b, err := json.Marshal(details)
	if err != nil {
		slog.ErrorContext(ctx, "password reset: marshal audit details", "error", err)
		b = []byte("{}")
	}
	s.auditLog.Log(ctx, audit.Entry{
		Action:   action,
		Resource: "user",
		UserID:   userID,
		Details:  b,
	})
}

// --- email body builders ---------------------------------------------------

// buildResetEmailPlainText is the Unisender-required plain-text fallback.
func buildResetEmailPlainText(confirmURL, token string) string {
	return fmt.Sprintf(`Здравствуйте!

Кто-то запросил восстановление пароля для вашего аккаунта в OneVoice.

Если это были вы — перейдите по ссылке (действительна 30 минут):
%s?token=%s

Если не вы — просто проигнорируйте это письмо, пароль не изменится.

С уважением,
команда OneVoice
`, confirmURL, token)
}

// buildResetEmailHTML is the rich-format variant with inline Linen-palette styles.
func buildResetEmailHTML(confirmURL, token string) string {
	return fmt.Sprintf(`<!doctype html>
<html><body style="font-family:-apple-system,system-ui,sans-serif;color:#2C2520;background:#F5E9D9;padding:24px">
  <h2 style="font-weight:500">Восстановление пароля</h2>
  <p>Здравствуйте! Кто-то запросил восстановление пароля для вашего аккаунта в OneVoice.</p>
  <p>Если это были вы — нажмите кнопку (ссылка действительна 30 минут):</p>
  <p style="margin:24px 0">
    <a href="%s?token=%s" style="display:inline-block;background:#D89B5A;color:#2C2520;padding:12px 24px;border-radius:10px;text-decoration:none">Задать новый пароль</a>
  </p>
  <p style="font-size:13px;color:#6B6258">Если не вы — просто проигнорируйте это письмо.</p>
</body></html>
`, confirmURL, token)
}
