// Package service — email_verification.go
//
// EmailVerificationService implements : prove ownership of the
// user's email address without breaking the Register auto-login flow
// Three orchestration methods:
//
// - RequestResend: rate-limited 1/min + 5/hr per user. Issues a
// fresh 32-byte token + enqueues the verification email inside one tx.
// - ConfirmVerify: atomic consume + flip
// users.email_verified=TRUE in one tx. Returns NO session material —
// no cookies, no JWT (T-VE-02 mitigation). Returns the userID for the
// handler to record on the audit row.
// - ChangeEmailBeforeVerify: escape hatch for users with a dead
// email-on-file. Allowed ONLY when email_verified=false. Updates
// users.email, invalidates ALL outstanding verification tokens, and
// issues a fresh one — all in one tx.
//
// Issuance is also exposed as IssueAndEnqueueTx so UserService.Register
// can call it inside the Register tx (token + outbox enqueue must
// commit atomically with the user_consents INSERT and the user row).
package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/repository"
)

const (
	// verifyTokenTTL — (RU email providers defer delivery hours;
	// 24h is too tight, 7d is too loose. 72h is the compromise.).
	verifyTokenTTL = 72 * time.Hour
	// verifyResendMinWindow / verifyResendHourMax / verifyResendHourWindow —
	// 1/min and 5/hr per user.
	verifyResendMinWindow  = 60 * time.Second
	verifyResendHourMax    = 5
	verifyResendHourWindow = 3600 * time.Second
	// verifyEmailSubject — RU primary per CONTEXT D-decisions + UI-SPEC Surface 12.
	verifyEmailSubject = "Подтвердите email — OneVoice"
)

// VerifyUserRepo is the slice of UserRepository the EmailVerificationService
// needs (interface segregation — handler/service tests pass a minimal mock).
type VerifyUserRepo interface {
	GetByID(ctx context.Context, userID uuid.UUID) (*domain.User, error)
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
	UpdateEmailInTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, newEmail string) error
	MarkEmailVerifiedInTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error
}

// EmailVerificationService — see file docstring.
type EmailVerificationService struct {
	pool      *pgxpool.Pool
	tokens    *repository.EmailVerificationTokenRepository
	users     VerifyUserRepo
	outbox    *repository.EmailOutboxRepository
	redis     *redis.Client
	publicURL string
	tokenTTL  time.Duration
}

// NewEmailVerificationService constructs the service. All deps
// are required.
func NewEmailVerificationService(
	pool *pgxpool.Pool,
	tokens *repository.EmailVerificationTokenRepository,
	users VerifyUserRepo,
	outbox *repository.EmailOutboxRepository,
	rd *redis.Client,
	publicURL string,
) *EmailVerificationService {
	return &EmailVerificationService{
		pool:      pool,
		tokens:    tokens,
		users:     users,
		outbox:    outbox,
		redis:     rd,
		publicURL: publicURL,
		tokenTTL:  verifyTokenTTL,
	}
}

// RequestResend: per-user 1/min + 5/hr Redis rate limit. Returns
// domain.ErrAlreadyVerified if the user is already verified (handler maps
// to HTTP 403) — saves the resend cost and signals UX that no banner
// should render.
func (s *EmailVerificationService) RequestResend(ctx context.Context, userID uuid.UUID) error {
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	if u.EmailVerified {
		return domain.ErrAlreadyVerified
	}

	minKey := fmt.Sprintf("verify_resend:user:%s:min", userID)
	cnt, err := s.redis.Incr(ctx, minKey).Result()
	if err != nil {
		return fmt.Errorf("verify_resend min incr: %w", err)
	}
	if cnt == 1 {
		_ = s.redis.Expire(ctx, minKey, verifyResendMinWindow).Err()
	} else if cnt > 1 {
		return domain.ErrResendThrottled
	}

	hrKey := fmt.Sprintf("verify_resend:user:%s:hr", userID)
	cnt, err = s.redis.Incr(ctx, hrKey).Result()
	if err != nil {
		return fmt.Errorf("verify_resend hr incr: %w", err)
	}
	if cnt == 1 {
		_ = s.redis.Expire(ctx, hrKey, verifyResendHourWindow).Err()
	} else if cnt > int64(verifyResendHourMax) {
		return domain.ErrResendThrottled
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("verify_resend begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := s.IssueAndEnqueueTx(ctx, tx, userID, u.Email); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ConfirmVerify: scanner-safe atomic consume. Returns the
// userID for the handler to record on the audit row's UserID. Returns
// (uuid.Nil, domain.ErrVerifyTokenInvalid) when the token is unknown,
// already consumed, or expired (PITFALLS §1.1 collapse).
//
// CRITICAL: this method must NEVER issue cookies / mutate session state.
// T-VE-02 mitigation — an attacker who registered with a victim's email
// must not become authenticated when the victim clicks the link.
func (s *EmailVerificationService) ConfirmVerify(ctx context.Context, plaintextToken string) (uuid.UUID, error) {
	hashArr := sha256.Sum256([]byte(plaintextToken))
	tokenHash := hashArr[:]

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("confirm_verify begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	userID, _, err := s.tokens.ConsumeAtomic(ctx, tx, tokenHash)
	if err != nil {
		return uuid.Nil, err
	}

	if err := s.users.MarkEmailVerifiedInTx(ctx, tx, userID); err != nil {
		return uuid.Nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, fmt.Errorf("confirm_verify commit: %w", err)
	}
	return userID, nil
}

// IsTokenExpired distinguishes invalid-vs-expired for the handler's UX
// branch (Surface 3). Returns (true, nil) if a row exists with
// consumed_at IS NULL AND expires_at <= NOW. Always returns (false, nil)
// for unknown / already-consumed tokens — they're indistinguishable from
// expired-then-consumed via the live atomic path, but the UX-side error
// code stays distinct purely as a hint to the user.
func (s *EmailVerificationService) IsTokenExpired(ctx context.Context, plaintextToken string) (bool, error) {
	hashArr := sha256.Sum256([]byte(plaintextToken))
	return s.tokens.LookupExpired(ctx, hashArr[:])
}

// ChangeEmailBeforeVerify: allowed ONLY when user.email_verified=false.
// Updates users.email, invalidates all outstanding verification tokens for
// the user, and issues a fresh token + outbox enqueue — all in one tx.
//
// Returns:
// - domain.ErrAlreadyVerified — handler maps to HTTP 403.
// - domain.ErrEmailTaken      — handler maps to HTTP 409 (UNIQUE-violation
// races to the same sentinel via UpdateEmailInTx).
// - oldEmail (first return) for the handler to write on the audit row.
func (s *EmailVerificationService) ChangeEmailBeforeVerify(ctx context.Context, userID uuid.UUID, newEmail string) (oldEmail string, err error) {
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return "", err
	}
	if u.EmailVerified {
		return "", domain.ErrAlreadyVerified
	}
	oldEmail = u.Email

	if existing, err := s.users.GetByEmail(ctx, newEmail); err == nil && existing != nil {
		return oldEmail, domain.ErrEmailTaken
	} else if err != nil && !errors.Is(err, domain.ErrUserNotFound) {
		return oldEmail, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return oldEmail, fmt.Errorf("change_email begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := s.users.UpdateEmailInTx(ctx, tx, userID, newEmail); err != nil {
		return oldEmail, err
	}
	if err := s.tokens.InvalidateAllForUser(ctx, tx, userID); err != nil {
		return oldEmail, fmt.Errorf("change_email invalidate tokens: %w", err)
	}
	if err := s.IssueAndEnqueueTx(ctx, tx, userID, newEmail); err != nil {
		return oldEmail, err
	}
	if err := tx.Commit(ctx); err != nil {
		return oldEmail, fmt.Errorf("change_email commit: %w", err)
	}
	return oldEmail, nil
}

// IssueAndEnqueueTx is the cross-call helper that issues a fresh
// verification token and enqueues the outbox row in the supplied tx.
// Exported so the user service can call it directly (it composes the
// user_consents INSERT + outbox enqueue + token issue in one tx).
func (s *EmailVerificationService) IssueAndEnqueueTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, email string) error {
	plaintext, hash, err := generateOpaqueToken()
	if err != nil {
		return fmt.Errorf("generate verify token: %w", err)
	}
	expiresAt := time.Now().Add(s.tokenTTL)
	if err := s.tokens.Insert(ctx, tx, userID, email, hash, expiresAt); err != nil {
		return fmt.Errorf("insert verify token: %w", err)
	}

	link := fmt.Sprintf("%s/auth/verify-email?token=%s", s.publicURL, plaintext)
	in := repository.OutboxEnqueueInput{
		ToEmail:  email,
		Subject:  verifyEmailSubject,
		BodyText: buildVerifyEmailPlainText(link),
		BodyHTML: buildVerifyEmailHTML(link),
	}
	if _, err := s.outbox.Enqueue(ctx, tx, in); err != nil {
		return fmt.Errorf("enqueue verify email: %w", err)
	}
	return nil
}

func buildVerifyEmailPlainText(link string) string {
	return fmt.Sprintf(`Здравствуйте!

Вы зарегистрировались в OneVoice. Подтвердите email, чтобы открыть полный
доступ — подключать интеграции и приглашать команду.

Подтвердить (ссылка действует 72 часа):
%s

Если вы не регистрировались в OneVoice — проигнорируйте это письмо.

С уважением,
команда OneVoice
`, link)
}

func buildVerifyEmailHTML(link string) string {
	return fmt.Sprintf(`<!doctype html>
<html><body style="font-family:Mona Sans,system-ui,sans-serif;color:#2C2520;background:#F5E9D9;padding:24px;">
<p>Здравствуйте!</p>
<p>Вы зарегистрировались в OneVoice. Подтвердите email, чтобы открыть полный доступ — подключать интеграции и приглашать команду.</p>
<p><a href="%s" style="display:inline-block;padding:12px 24px;background:#D89B5A;color:#2C2520;text-decoration:none;border-radius:10px;">Подтвердить email</a></p>
<p style="color:#7a6f60;font-size:13px;">Ссылка действует 72 часа. Если вы не регистрировались в OneVoice — проигнорируйте это письмо.</p>
</body></html>`, link)
}
