package oauthlock

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/google/uuid"
	pgx "github.com/jackc/pgx/v5"
)

// LockExecutor is satisfied by *pgxpool.Pool and pgxmock.PgxPoolIface.
type LockExecutor interface {
	BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error)
}

// WithRefreshLock opens a transaction, tries to acquire
// pg_try_advisory_xact_lock keyed by integrationID, and calls fn inside the
// transaction. The lock is released automatically on commit or rollback.
// Returns ErrLockBusy immediately when the lock is held by another connection.
func WithRefreshLock(
	ctx context.Context,
	pool LockExecutor,
	integrationID uuid.UUID,
	platform string,
	fn func(context.Context, pgx.Tx) error,
) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("oauthlock: begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	var acquired bool
	if err := tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock($1)", lockKey(integrationID)).Scan(&acquired); err != nil {
		return fmt.Errorf("oauthlock: try-lock: %w", err)
	}
	if !acquired {
		ContendedTotal.WithLabelValues(platform).Inc()
		return ErrLockBusy
	}

	if err := fn(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("oauthlock: commit: %w", err)
	}
	committed = true
	return nil
}

// RefreshWithRetry retries WithRefreshLock with exponential backoff on
// ErrLockBusy. Returns ErrLockExhausted after all slots are exhausted.
func RefreshWithRetry(
	ctx context.Context,
	pool LockExecutor,
	integrationID uuid.UUID,
	platform string,
	fn func(context.Context, pgx.Tx) error,
) error {
	return RefreshWithRetryFn(ctx, pool, integrationID, platform, fn, WithRefreshLock)
}

// RefreshWithRetryFn is RefreshWithRetry with an injectable lock function for
// testing. Production callers should use RefreshWithRetry.
func RefreshWithRetryFn(
	ctx context.Context,
	pool LockExecutor,
	integrationID uuid.UUID,
	platform string,
	fn func(context.Context, pgx.Tx) error,
	lockFn func(context.Context, LockExecutor, uuid.UUID, string, func(context.Context, pgx.Tx) error) error,
) error {
	const jitterDivisor = 4
	backoffs := []time.Duration{
		100 * time.Millisecond,
		300 * time.Millisecond,
		1 * time.Second,
		1500 * time.Millisecond,
	}
	var lastErr error
	for i, wait := range backoffs {
		err := lockFn(ctx, pool, integrationID, platform, fn)
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrLockBusy) {
			return err
		}
		lastErr = err
		if i == len(backoffs)-1 {
			break
		}
		jitter := time.Duration(rand.Int64N(int64(wait) / jitterDivisor)) //nolint:gosec // jitter is non-security; math/rand/v2 acceptable

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait + jitter):
		}
	}
	ExhaustedTotal.WithLabelValues(platform).Inc()
	_ = lastErr
	return ErrLockExhausted
}

// lockKey converts an integration UUID to a stable int64 suitable for
// pg_try_advisory_xact_lock. Uses SHA-256 of "oauth:refresh:" prefix + UUID
// bytes, taking the first 8 bytes as a little-endian int64.
func lockKey(id uuid.UUID) int64 {
	prefix := []byte("oauth:refresh:")
	h := sha256.Sum256(append(prefix, id[:]...))
	return int64(binary.LittleEndian.Uint64(h[:8])) //nolint:gosec // sha256→int64 is intentional bit reinterpretation for advisory lock id
}
