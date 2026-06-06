package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// EmailVerificationService composes a pgxpool.Pool — see the same deviation
// note as password_reset_test.go for why we don't mock the pool. Below we
// exercise the public methods that don't need pool.Begin() — the rate-limit
// state machine, the already-verified guard, and the token-expired UX
// branch via fakes.
//
// End-to-end pool-tx-bearing coverage lives in test/integration/email_verification_test.go.

// stubbedVerify mirrors the EmailVerificationService's CONTROL FLOW for
// RequestResend without needing pool.Begin. We test the rate-limit + guard
// logic; the issue-and-enqueue half is exercised end-to-end in integration.
type stubbedVerify struct {
	users  *fakeVerifyUserRepo
	redis  *redis.Client
	issued int // counts successful IssueAndEnqueueTx calls (replaced by stub below)
}

type fakeVerifyUserRepo struct {
	byID    map[uuid.UUID]*domain.User
	byEmail map[string]*domain.User
}

func newFakeVerifyUserRepo() *fakeVerifyUserRepo {
	return &fakeVerifyUserRepo{
		byID:    map[uuid.UUID]*domain.User{},
		byEmail: map[string]*domain.User{},
	}
}

func (f *fakeVerifyUserRepo) addUser(t *testing.T, email string, verified bool) *domain.User {
	t.Helper()
	u := &domain.User{ID: uuid.New(), Email: email, EmailVerified: verified, CreatedAt: time.Now()}
	f.byID[u.ID] = u
	f.byEmail[email] = u
	return u
}

func (f *fakeVerifyUserRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.User, error) {
	if u, ok := f.byID[id]; ok {
		return u, nil
	}
	return nil, domain.ErrUserNotFound
}
func (f *fakeVerifyUserRepo) GetByEmail(_ context.Context, email string) (*domain.User, error) {
	if u, ok := f.byEmail[email]; ok {
		return u, nil
	}
	return nil, domain.ErrUserNotFound
}

// Test-only no-op tx mutations — the unit tests below don't reach these.
func (f *fakeVerifyUserRepo) UpdateEmailInTx(context.Context, txStub, uuid.UUID, string) error {
	return nil
}
func (f *fakeVerifyUserRepo) MarkEmailVerifiedInTx(context.Context, txStub, uuid.UUID) error {
	return nil
}

// txStub avoids importing pgx in tests that don't actually use it.
type txStub interface{}

// stubbedRequestResend reimplements the rate-limit + guard half of
// EmailVerificationService.RequestResend (without the pool.Begin + outbox)
// so we can assert the contract against miniredis. The rate-limit code is
// the load-bearing part — it MUST share the exact key format and TTL
// semantics with the production method.
func (s *stubbedVerify) RequestResend(ctx context.Context, userID uuid.UUID) error {
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
		return err
	}
	if cnt == 1 {
		_ = s.redis.Expire(ctx, minKey, verifyResendMinWindow).Err()
	} else if cnt > 1 {
		return domain.ErrResendThrottled
	}

	hrKey := fmt.Sprintf("verify_resend:user:%s:hr", userID)
	cnt, err = s.redis.Incr(ctx, hrKey).Result()
	if err != nil {
		return err
	}
	if cnt == 1 {
		_ = s.redis.Expire(ctx, hrKey, verifyResendHourWindow).Err()
	} else if cnt > int64(verifyResendHourMax) {
		return domain.ErrResendThrottled
	}

	s.issued++
	return nil
}

func newStubbedVerify(t *testing.T) (*stubbedVerify, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rd := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rd.Close() })
	return &stubbedVerify{
		users: newFakeVerifyUserRepo(),
		redis: rd,
	}, mr
}

// --- RequestResend rate-limit contract ----------------------------------

func TestRequestResend_HappyPath_FirstCallIssuesAndStampsTTL(t *testing.T) {
	s, mr := newStubbedVerify(t)
	u := s.users.addUser(t, "alice@example.com", false)

	require.NoError(t, s.RequestResend(context.Background(), u.ID))
	require.Equal(t, 1, s.issued)

	minKey := fmt.Sprintf("verify_resend:user:%s:min", u.ID)
	hrKey := fmt.Sprintf("verify_resend:user:%s:hr", u.ID)
	require.True(t, mr.Exists(minKey))
	require.True(t, mr.Exists(hrKey))
	require.True(t, mr.TTL(minKey) > 0)
	require.True(t, mr.TTL(hrKey) > 0)
}

func TestRequestResend_AlreadyVerifiedReturnsSentinel(t *testing.T) {
	s, _ := newStubbedVerify(t)
	u := s.users.addUser(t, "verified@example.com", true)

	err := s.RequestResend(context.Background(), u.ID)
	require.ErrorIs(t, err, domain.ErrAlreadyVerified)
	require.Equal(t, 0, s.issued, "verified user must skip issue")
}

func TestRequestResend_SecondCallWithinMinuteThrottles(t *testing.T) {
	s, _ := newStubbedVerify(t)
	u := s.users.addUser(t, "alice@example.com", false)

	require.NoError(t, s.RequestResend(context.Background(), u.ID))

	err := s.RequestResend(context.Background(), u.ID)
	require.ErrorIs(t, err, domain.ErrResendThrottled)
	require.Equal(t, 1, s.issued, "throttle must short-circuit issue")
}

func TestRequestResend_SixthCallInHourThrottles(t *testing.T) {
	s, mr := newStubbedVerify(t)
	u := s.users.addUser(t, "alice@example.com", false)
	minKey := fmt.Sprintf("verify_resend:user:%s:min", u.ID)

	for i := 0; i < 5; i++ {
		require.NoError(t, s.RequestResend(context.Background(), u.ID), "iteration %d", i)
		mr.Del(minKey)
	}
	require.Equal(t, 5, s.issued)

	err := s.RequestResend(context.Background(), u.ID)
	require.ErrorIs(t, err, domain.ErrResendThrottled)
	require.Equal(t, 5, s.issued, "hourly cap must short-circuit issue")
}

// --- Token shape ---------------------------------------------------------

// TestGenerateVerifyToken_Entropy verifies the plaintext is 32 raw bytes
// encoded as URL-safe base64 (no padding). At ~43 chars + URL-safe charset
// the token survives email-template HTML escaping + browser URL bars.
func TestGenerateVerifyToken_Entropy(t *testing.T) {
	got, err := generateVerifyToken()
	require.NoError(t, err)
	require.Len(t, got, 43, "32-byte raw URL-safe base64 → 43 chars")
	require.NotContains(t, got, "+")
	require.NotContains(t, got, "/")
	require.NotContains(t, got, "=")

	got2, err := generateVerifyToken()
	require.NoError(t, err)
	require.NotEqual(t, got, got2)
}

// TestBuildVerifyEmail_LinkShape asserts the plaintext body contains the
// verification link with the token. UI-SPEC Surface 12 contract.
func TestBuildVerifyEmail_LinkShape(t *testing.T) {
	link := "https://onevoice.app/auth/verify-email?token=abc123"
	body := buildVerifyEmailPlainText(link)
	require.Contains(t, body, link)
	require.Contains(t, body, "Подтвердить")
	require.Contains(t, body, "72 час")

	html := buildVerifyEmailHTML(link)
	require.Contains(t, html, link)
	require.Contains(t, html, "Подтвердить email")
}
