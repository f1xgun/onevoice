package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type ownerBriefLockPool interface {
	Begin(context.Context) (pgx.Tx, error)
}

// OwnerBriefLock serializes complete weekly-brief passes across API replicas.
// The transaction pins the advisory lock to a single PostgreSQL connection.
type OwnerBriefLock struct{ pool ownerBriefLockPool }

func NewOwnerBriefLock(pool ownerBriefLockPool) *OwnerBriefLock { return &OwnerBriefLock{pool: pool} }

func (l *OwnerBriefLock) WithOwnerBriefLock(ctx context.Context, fn func() error) (bool, error) {
	tx, err := l.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin owner brief lock: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var acquired bool
	if err := tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtext('onevoice.owner-brief'))").Scan(&acquired); err != nil {
		return false, fmt.Errorf("acquire owner brief lock: %w", err)
	}
	if !acquired {
		return false, nil
	}
	if err := fn(); err != nil {
		return true, fmt.Errorf("run locked owner brief pass: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return true, fmt.Errorf("commit owner brief lock: %w", err)
	}
	return true, nil
}
