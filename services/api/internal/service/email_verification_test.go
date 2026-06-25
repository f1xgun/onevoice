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
	"github.com/f1xgun/onevoice/pkg/ratelimit"
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

// stubbedRequestResend mirrors the rate-limit + guard half of
// EmailVerificationService.RequestResend (without the pool.Begin + outbox)
// so we can assert the contract against miniredis. It shares the exact key
// format and the same ratelimit.IncrWithHeal helper as the production method,
// so the load-bearing throttle code stays covered.
func (s *stubbedVerify) RequestResend(ctx context.Context, userID uuid.UUID) error {
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	if u.EmailVerified {
		return domain.ErrAlreadyVerified
	}

	minKey := fmt.Sprintf("verify_resend:user:%s:min", userID)
	cnt, err := ratelimit.IncrWithHeal(ctx, s.redis, minKey, verifyResendMinWindow)
	if err != nil {
		return err
	}
	if cnt > 1 {
		return domain.ErrResendThrottled
	}

	hrKey := fmt.Sprintf("verify_resend:user:%s:hr", userID)
	cnt, err = ratelimit.IncrWithHeal(ctx, s.redis, hrKey, verifyResendHourWindow)
	if err != nil {
		return err
	}
	if cnt > int64(verifyResendHourMax) {
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

// TestRequestResend_SelfHealsMissingTTL reproduces the lost-EXPIRE failure:
// the per-minute counter loses its TTL (a transient Redis blip when the key
// was first stamped), so without the heal it stays at value 1 with no expiry
// and every later call INCRs to >1 → throttled forever, blocking onboarding.
// The heal re-stamps the TTL on the next call so the window can recover. With
// the raw INCR + conditional-Expire this test fails: the counter stays
// TTL-less ("min counter must always carry a TTL so the throttle can recover").
func TestRequestResend_SelfHealsMissingTTL(t *testing.T) {
	s, mr := newStubbedVerify(t)
	u := s.users.addUser(t, "alice@example.com", false)
	minKey := fmt.Sprintf("verify_resend:user:%s:min", u.ID)

	require.NoError(t, s.RequestResend(context.Background(), u.ID))
	require.Positive(t, mr.TTL(minKey).Nanoseconds(), "first call must stamp a TTL")

	require.NoError(t, s.redis.Persist(context.Background(), minKey).Err())
	require.Equal(t, time.Duration(0), mr.TTL(minKey), "precondition: counter has no TTL")

	err := s.RequestResend(context.Background(), u.ID)
	require.ErrorIs(t, err, domain.ErrResendThrottled, "second call within the window is still throttled")
	require.Positive(t, mr.TTL(minKey).Nanoseconds(), "min counter must always carry a TTL so the throttle can recover")

	mr.FastForward(verifyResendMinWindow + time.Second)
	require.False(t, mr.Exists(minKey), "repaired window must expire so resend recovers")
	require.NoError(t, s.RequestResend(context.Background(), u.ID), "after the window the user can resend again")
}

// --- Token shape ---------------------------------------------------------

// TestGenerateVerifyToken_Entropy verifies the plaintext is 32 raw bytes
// encoded as URL-safe base64 (no padding). At ~43 chars + URL-safe charset
// the token survives email-template HTML escaping + browser URL bars.
func TestGenerateVerifyToken_Entropy(t *testing.T) {
	got, hash, err := generateOpaqueToken()
	require.NoError(t, err)
	require.Len(t, got, 43, "32-byte raw URL-safe base64 → 43 chars")
	require.Len(t, hash, 32, "sha-256 hash is 32 bytes")
	require.NotContains(t, got, "+")
	require.NotContains(t, got, "/")
	require.NotContains(t, got, "=")

	got2, _, err := generateOpaqueToken()
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
