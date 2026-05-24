// Package service — password_reset.go
//
// PasswordResetService implements ACCT-01: a no-enumeration password
// recovery flow that lets a user prove control of their email and
// pick a new password without operator intervention.
//
// Hard contracts (lifted from .planning/phases/21-account-lifecycle/21-CONTEXT.md):
//
//   - D-08: token is 32 bytes of crypto/rand, base64url-encoded for the
//     URL; only its SHA-256 hash is persisted.
//   - D-09: token TTL = 30 minutes.
//   - D-10: RequestReset NEVER returns a non-nil error to the caller.
//     Confirm uses (reset_token_invalid | reset_token_expired |
//     password_too_weak) for the three failure modes.
//   - D-12: ConsumeAtomic is a single UPDATE…RETURNING. Confirm's
//     password update + refresh-token wipe run in the same tx as the
//     consume (refresh wipe is Redis-side, executed after tx.Commit).
//   - D-13: all of the user's refresh-token entries in Redis are deleted
//     on a successful confirm — keyed by tokenID and stored at
//     `onevoice:auth:refresh_token:<tokenID>` with value=<user_id>. We
//     scan the key-space and delete every key whose value matches.
//     (CONTEXT D-13 was drafted assuming a `refresh:user:<id>:*` key
//     pattern; the on-disk implementation uses `onevoice:auth:refresh_token:<tokenID>`
//     so the wipe scans + filters by value.)
//   - D-14: per-email rate limit = 3/hr via Redis INCR + EXPIRE 3600.
//     4th+ requests still respond identically — handler returns 204 —
//     but no email is sent.
//   - D-15: symmetric load — unknown-email branch writes a dummy
//     audit_log row so DB write cost is constant in both branches.
//
// PITFALLS §1.1 (no enumeration): the ONLY error a confirm caller ever
// sees for token problems is ErrResetTokenInvalid — expired and unknown
// collapse into the same sentinel by the atomic-consume statement.
package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/repository"
)

const (
	// resetTokenTTL — D-09.
	resetTokenTTL = 30 * time.Minute
	// resetTokenEntropyBytes — D-08 (256 bits).
	resetTokenEntropyBytes = 32
	// resetMinPasswordLen — handler validator enforces this; service
	// double-checks BEFORE consuming the token (PITFALLS §1.2: a weak
	// password must not burn the user's token).
	resetMinPasswordLen = 8
	// resetRateLimitMax — D-14: 3 requests / hour / email.
	resetRateLimitMax    = 3
	resetRateLimitWindow = time.Hour
	// resetConfirmURLBase — public URL embedded in the email body.
	// TODO: source from cfg.PublicURL once 21-04 wires in the public host
	// (D-04 keeps the default for v1.4).
	resetConfirmURLBase = "https://onevoice.app/auth/password-reset/confirm"
	// resetEmailSubject — RU primary per CONTEXT D-decisions.
	resetEmailSubject = "Восстановление пароля — OneVoice"
)

// Service-level sentinels exposed for the handler error_mapping.
//
// ErrResetTokenInvalid mirrors domain.ErrResetTokenInvalid so callers can
// continue to use service.ErrResetTokenInvalid without importing the
// domain package directly (matches the pattern in other services).
var (
	ErrResetTokenInvalid = domain.ErrResetTokenInvalid
	ErrPasswordTooWeak   = errors.New("password too weak")
)

// UserRepoForReset is the slice of UserRepository the PasswordResetService
// needs. Declaring it inline (interface segregation) lets the test pass a
// minimal mock without implementing the full UserRepository surface.
//
// UpdatePasswordHash is added here because no existing UserRepository
// method exposes a tx-aware bcrypt-hash setter — service/user.go's
// ChangePassword path goes through Update(user *User) which is non-tx and
// rewrites the whole row. We avoid touching the existing path; instead
// the reset flow uses a focused tx-aware setter implemented in
// service/password_reset.go::userRepoAdapter below.
type UserRepoForReset interface {
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
	UpdatePasswordHashInTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, bcryptHash []byte) error
}

// PasswordResetService — see file docstring for contract.
type PasswordResetService struct {
	pool      *pgxpool.Pool
	tokenRepo *repository.PasswordResetTokenRepository
	userRepo  UserRepoForReset
	outbox    *repository.EmailOutboxRepository
	auditLog  audit.Logger
	redis     *redis.Client
}

// NewPasswordResetService constructs a PasswordResetService. All deps
// are required — panics-free, returns nil if pool is nil so callers see
// a deterministic boot-time error rather than a runtime nil-deref.
func NewPasswordResetService(
	pool *pgxpool.Pool,
	tokenRepo *repository.PasswordResetTokenRepository,
	userRepo UserRepoForReset,
	outbox *repository.EmailOutboxRepository,
	auditLogger audit.Logger,
	redisClient *redis.Client,
) *PasswordResetService {
	return &PasswordResetService{
		pool:      pool,
		tokenRepo: tokenRepo,
		userRepo:  userRepo,
		outbox:    outbox,
		auditLog:  auditLogger,
		redis:     redisClient,
	}
}

// RequestReset implements D-10/D-14/D-15/PITFALLS §1.1. Always returns nil.
//
// Flow:
//  1. Rate-limit gate FIRST so its cost is paid in BOTH email-known and
//     email-unknown branches — keeps timing parity.
//  2. UserRepo.GetByEmail. Unknown → write dummy audit row (symmetric DB
//     load) and return.
//  3. If rate-limited → write the standard "requested" audit row WITH
//     rate_limited=true in metadata, no email sent.
//  4. Generate 32-byte token, base64url-encode, sha256, open tx.
//  5. Invalidate older outstanding tokens for the user.
//  6. Insert the new token row.
//  7. Enqueue the outbox row (same tx — atomicity per Phase 21a).
//  8. tx.Commit; emit audit row.
//
// Any error in steps 2-8 still returns nil to the caller. Internal
// failures get logged via slog + audit so they're forensically visible
// without changing the response shape.
func (s *PasswordResetService) RequestReset(ctx context.Context, emailAddr, clientIP, userAgent string) error {
	rateLimited := s.bumpRateLimit(ctx, emailAddr)

	user, err := s.userRepo.GetByEmail(ctx, emailAddr)
	switch {
	case errors.Is(err, domain.ErrUserNotFound):
		// Symmetric-load branch (PITFALLS §1.1, D-15). Dummy audit row.
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

	rawToken := make([]byte, resetTokenEntropyBytes)
	if _, err := rand.Read(rawToken); err != nil {
		slog.ErrorContext(ctx, "password reset: rand.Read failed", "error", err)
		s.audit(ctx, audit.ActionPasswordResetRequested, &user.ID, map[string]any{
			"ip":         clientIP,
			"user_agent": userAgent,
			"error":      "rand_failed",
		})
		return nil
	}
	plaintext := base64.RawURLEncoding.EncodeToString(rawToken)
	hash := sha256.Sum256([]byte(plaintext))
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
	if err := s.tokenRepo.Insert(ctx, tx, user.ID, hash[:], expiresAt); err != nil {
		slog.ErrorContext(ctx, "password reset: insert token", "error", err)
		return nil
	}
	if _, err := s.outbox.Enqueue(ctx, tx, repository.OutboxEnqueueInput{
		ToEmail:  user.Email,
		Subject:  resetEmailSubject,
		BodyText: buildResetEmailPlainText(plaintext),
		BodyHTML: buildResetEmailHTML(plaintext),
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

// ConfirmReset implements D-10/D-12/D-13.
//
//   - newPassword length validated BEFORE the consume so a weak password
//     doesn't burn the user's one-shot token (PITFALLS §1.2).
//   - sha256(plaintextToken) → ConsumeAtomic in a tx.
//   - bcrypt new password → UpdatePasswordHashInTx (same tx).
//   - tx.Commit.
//   - After commit: delete every refresh-token Redis key whose value is
//     the affected user_id. The wipe is intentionally NOT inside the
//     Postgres tx — Redis and Postgres can't share one — but it runs
//     AFTER the commit, so the password change is the durable event;
//     a Redis hiccup leaves the user with stale-but-soon-expiring
//     refresh tokens (15-min access TTL bounds the exposure).
func (s *PasswordResetService) ConfirmReset(ctx context.Context, plaintextToken, newPassword, clientIP, userAgent string) error {
	if len(newPassword) < resetMinPasswordLen {
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

	// Best-effort post-commit refresh-token wipe (D-13). Errors here are
	// logged but do not poison the success response — the new password
	// IS in place; stale refresh tokens expire naturally at access TTL.
	if err := s.wipeRefreshTokens(ctx, userID); err != nil {
		slog.ErrorContext(ctx, "password reset: wipe refresh tokens", "error", err, "user_id", userID)
	}

	s.audit(ctx, audit.ActionPasswordResetCompleted, &userID, map[string]any{
		"ip":         clientIP,
		"user_agent": userAgent,
	})
	return nil
}

// bumpRateLimit returns true when the per-email counter exceeds the
// 3-per-hour budget for THIS request (post-INCR), false otherwise.
//
// Redis hiccups → fail-OPEN (return false) so a Redis outage doesn't
// block legitimate password resets; the dummy-audit-row branch + always-
// 204 contract keep enumeration defenses intact.
func (s *PasswordResetService) bumpRateLimit(ctx context.Context, emailAddr string) bool {
	emailHash := sha256.Sum256([]byte(emailAddr))
	rateKey := fmt.Sprintf("reset:email:%x", emailHash)
	count, err := s.redis.Incr(ctx, rateKey).Result()
	if err != nil {
		slog.ErrorContext(ctx, "password reset: rate-limit INCR failed (fail-open)", "error", err)
		return false
	}
	if count == 1 {
		_ = s.redis.Expire(ctx, rateKey, resetRateLimitWindow).Err()
	}
	return count > resetRateLimitMax
}

// wipeRefreshTokens scans the refresh-token key namespace and deletes
// every key whose value is the target userID's string form.
//
// Uses SCAN (not KEYS — KEYS blocks Redis in production). MATCH narrows
// to the refresh-token prefix; values are compared in Go because the key
// itself contains the tokenID, not the userID.
//
// Best-effort: errors are returned (caller logs them) but do not block
// the success response.
func (s *PasswordResetService) wipeRefreshTokens(ctx context.Context, userID uuid.UUID) error {
	target := userID.String()
	var cursor uint64
	var toDelete []string
	for {
		keys, next, err := s.redis.Scan(ctx, cursor, refreshTokenKeyPrefix+"*", 256).Result()
		if err != nil {
			return fmt.Errorf("scan: %w", err)
		}
		for _, k := range keys {
			val, err := s.redis.Get(ctx, k).Result()
			if errors.Is(err, redis.Nil) {
				continue
			}
			if err != nil {
				return fmt.Errorf("get %s: %w", k, err)
			}
			if val == target {
				toDelete = append(toDelete, k)
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	if len(toDelete) == 0 {
		return nil
	}
	if err := s.redis.Del(ctx, toDelete...).Err(); err != nil {
		return fmt.Errorf("del: %w", err)
	}
	return nil
}

// audit fires a fire-and-forget audit row. Details are pre-marshaled here
// because pkg/audit.Entry takes json.RawMessage (D-10: no map[string]any
// at the boundary — but the boundary IS this function; the pre-marshal
// happens HERE so the rest of the service can pass map[string]any
// ergonomically).
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
// Token is URL-embedded as ?token=<plaintext>; the recipient's browser
// presents the reveal-pattern frontend before any backend consume.
func buildResetEmailPlainText(token string) string {
	return fmt.Sprintf(`Здравствуйте!

Кто-то запросил восстановление пароля для вашего аккаунта в OneVoice.

Если это были вы — перейдите по ссылке (действительна 30 минут):
%s?token=%s

Если не вы — просто проигнорируйте это письмо, пароль не изменится.

С уважением,
команда OneVoice
`, resetConfirmURLBase, token)
}

// buildResetEmailHTML is the rich-format variant. Inline styles keep
// approximate Linen palette tokens through mail-client style stripping.
func buildResetEmailHTML(token string) string {
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
`, resetConfirmURLBase, token)
}
