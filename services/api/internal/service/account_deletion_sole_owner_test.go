package service

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/repository"
)

// ownerRoleID is the system owner-role UUID seeded by the RBAC migration; it is
// the FK target both the sole-owner gate and the BEFORE-DELETE trigger key off.
const ownerRoleID = "00000000-0000-0000-0000-000000000001"

// noMongoConversations satisfies domain.ConversationRepository while stubbing
// out the only method the sweeper calls post-commit (MongoConversationsCleanup),
// so these Postgres-backed tests need no Mongo. The embedded nil interface is
// never dereferenced because the sweeper touches nothing else.
type noMongoConversations struct {
	domain.ConversationRepository
}

func (noMongoConversations) MongoConversationsCleanup(_ context.Context, _, _ string) (int64, error) {
	return 0, nil
}

// soleOwnerHarness opens a pool against the fully-migrated TEST_POSTGRES_URL DB
// (the sole-owner gate query and the fn_refuse_sole_owner_delete trigger both
// require the real schema) and constructs an AccountDeletionService wired to the
// concrete user adapter. Returns a grace-compressed service so the sweeper can
// claim freshly-seeded rows.
func soleOwnerHarness(t *testing.T) (context.Context, *pgxpool.Pool, *AccountDeletionService) {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_URL not set; skipping sole-owner deletion integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	svc := NewAccountDeletionService(
		pool,
		repository.NewUserResetExtAdapter(pool),
		noMongoConversations{},
		repository.NewEmailOutboxRepository(pool),
		audit.Nop(),
	).WithGraceDays(0, 0)
	return ctx, pool, svc
}

// seedOwner inserts a verified user and an active owner-role membership of biz.
func seedOwner(ctx context.Context, t *testing.T, pool *pgxpool.Pool, biz uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, password_hash, email_verified) VALUES ($1, $2, 'x', TRUE)`,
		id, id.String()+"@sole-owner.test")
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO business_members (business_id, user_id, role_id, status, joined_at) VALUES ($1, $2, $3::uuid, 'active', now())`,
		biz, id, ownerRoleID)
	require.NoError(t, err)
	return id
}

// markPending stamps deletion_requested_at + deleted_at on the user, matching
// what RequestDeletionInTx writes, with deletion_requested_at set to requestedAt
// (used to order the sweeper batch and push the row past the grace window).
func markPending(ctx context.Context, t *testing.T, pool *pgxpool.Pool, userID uuid.UUID, requestedAt time.Time) {
	t.Helper()
	_, err := pool.Exec(ctx,
		`UPDATE users SET deletion_requested_at = $2, deleted_at = $2, deletion_canceled_at = NULL WHERE id = $1`,
		userID, requestedAt)
	require.NoError(t, err)
}

// TestEnumerateSoleOwnerBusinesses_PendingCoOwnerIsNotEffective is the
// fail-on-revert guard for the gate fix. Business B has two owner members A and
// B. After A requests deletion (soft-deleted but membership row intact), B must
// be seen as the SOLE EFFECTIVE owner: RequestDeletion(B) is rejected with
// *ErrSoleOwnerBusinesses naming B. Reverting the gate to a raw COUNT(*) over
// business_members (no pending-owner filter) lets B's request through — the
// trigger would then abort the whole hard-delete batch downstream.
func TestEnumerateSoleOwnerBusinesses_PendingCoOwnerIsNotEffective(t *testing.T) {
	ctx, pool, svc := soleOwnerHarness(t)

	biz := uuid.New()
	_, err := pool.Exec(ctx,
		`INSERT INTO businesses (id, name) VALUES ($1, 'SoleOwnerGateBiz')`,
		biz)
	require.NoError(t, err)

	ownerA := seedOwner(ctx, t, pool, biz)
	ownerB := seedOwner(ctx, t, pool, biz)

	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM business_members WHERE business_id = $1`, biz)
		_, _ = pool.Exec(c, `DELETE FROM businesses WHERE id = $1`, biz)
		_, _ = pool.Exec(c, `DELETE FROM users WHERE id IN ($1, $2)`, ownerA, ownerB)
	})

	require.NoError(t, svc.RequestDeletion(ctx, ownerA, "", "127.0.0.1", "test", "consent_withdrawn"),
		"first co-owner deletion must be allowed (B is still an effective owner)")

	err = svc.RequestDeletion(ctx, ownerB, "", "127.0.0.1", "test", "consent_withdrawn")
	var soleErr *ErrSoleOwnerBusinesses
	require.ErrorAs(t, err, &soleErr,
		"second co-owner must be the SOLE EFFECTIVE owner once the first is pending — reverting the gate filter fails open here")
	require.Len(t, soleErr.Businesses, 1)
	assert.Equal(t, biz, soleErr.Businesses[0].ID)
}

// TestHardDeleteSweeper_IsolatesTriggerAbortPerUser is the fail-on-revert guard
// for the SAVEPOINT fix. It reproduces a legacy pre-fix pending pair: business B
// has two pending owners (A older, B newer) plus an independent pending user C.
// Hard-deleting A succeeds; CASCADE drops A's membership; hard-deleting B then
// leaves B the sole owner and the BEFORE-DELETE trigger RAISEs P0001. Without a
// per-user SAVEPOINT that exception poisons the whole batch tx → Commit fails →
// 0 purged → the pipeline wedges. With the SAVEPOINT only B's sub-tx rolls back;
// A and C are purged and the batch commits.
func TestHardDeleteSweeper_IsolatesTriggerAbortPerUser(t *testing.T) {
	ctx, pool, svc := soleOwnerHarness(t)

	biz := uuid.New()
	_, err := pool.Exec(ctx,
		`INSERT INTO businesses (id, name) VALUES ($1, 'SoleOwnerSweepBiz')`,
		biz)
	require.NoError(t, err)

	ownerA := seedOwner(ctx, t, pool, biz)
	ownerB := seedOwner(ctx, t, pool, biz)

	indep := uuid.New()
	_, err = pool.Exec(ctx,
		`INSERT INTO users (id, email, password_hash, email_verified) VALUES ($1, $2, 'x', TRUE)`,
		indep, indep.String()+"@sole-owner.test")
	require.NoError(t, err)

	now := time.Now()
	markPending(ctx, t, pool, ownerA, now.Add(-72*time.Hour))
	markPending(ctx, t, pool, ownerB, now.Add(-71*time.Hour))
	markPending(ctx, t, pool, indep, now.Add(-70*time.Hour))

	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM business_members WHERE business_id = $1`, biz)
		_, _ = pool.Exec(c, `DELETE FROM businesses WHERE id = $1`, biz)
		_, _ = pool.Exec(c, `DELETE FROM users WHERE id IN ($1, $2, $3)`, ownerA, ownerB, indep)
	})

	count, err := svc.HardDeleteSweeper(ctx)
	require.NoError(t, err,
		"a per-user trigger abort must not wedge the whole batch — without the SAVEPOINT the commit fails here")
	assert.GreaterOrEqual(t, count, 2, "the non-conflicting users must be purged despite the trigger abort")

	for _, id := range []uuid.UUID{ownerA, indep} {
		var exists bool
		require.NoError(t, pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)`, id).Scan(&exists))
		assert.False(t, exists, "non-conflicting pending user %s should be purged", id)
	}

	var bStillThere bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)`, ownerB).Scan(&bStillThere))
	assert.True(t, bStillThere, "the trigger-blocked owner survives its rolled-back sub-tx")
}
