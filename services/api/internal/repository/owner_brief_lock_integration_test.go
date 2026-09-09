//go:build integration

package repository

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOwnerBriefLockSerializesAndReleasesAfterFailure(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()
	first, second := NewOwnerBriefLock(pool), NewOwnerBriefLock(pool)
	failure := errors.New("pass failed")
	locked, err := first.WithOwnerBriefLock(ctx, func() error {
		entered := false
		otherLocked, otherErr := second.WithOwnerBriefLock(ctx, func() error { entered = true; return nil })
		require.NoError(t, otherErr)
		assert.False(t, otherLocked)
		assert.False(t, entered)
		return failure
	})
	assert.True(t, locked)
	require.ErrorIs(t, err, failure)
	entered := false
	locked, err = second.WithOwnerBriefLock(ctx, func() error { entered = true; return nil })
	require.NoError(t, err)
	assert.True(t, locked)
	assert.True(t, entered)

	cancelledCtx, cancelPass := context.WithCancel(ctx)
	locked, err = first.WithOwnerBriefLock(cancelledCtx, func() error {
		cancelPass()
		return cancelledCtx.Err()
	})
	assert.True(t, locked)
	require.ErrorIs(t, err, context.Canceled)
	locked, err = second.WithOwnerBriefLock(ctx, func() error { return nil })
	require.NoError(t, err)
	assert.True(t, locked)
}
