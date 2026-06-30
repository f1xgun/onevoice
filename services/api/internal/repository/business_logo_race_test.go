package repository

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// TestBusinessRepository_LogoAndProfileWriters_NoLostUpdate is the fail-on-revert
// guard for the logo/profile cross-field clobber.
//
// Two writers touch DIFFERENT columns from snapshots taken before the other
// committed: writer A (a profile edit) loads the row while logo_url is still
// 'old' and later commits a name change, while writer B (a logo upload) commits
// logo_url='new' in between. With the old whole-row Update (which re-persisted
// the editor's stale logo_url snapshot) writer A's commit reverts logo_url back
// to 'old' — pointing the dashboard at a deleted object and losing the freshly
// uploaded logo. With the targeted writes (UpdateLogoURL touches only logo_url,
// Update omits logo_url) both survive.
func TestBusinessRepository_LogoAndProfileWriters_NoLostUpdate(t *testing.T) {
	ctx := context.Background()
	pool, repo := settingsRacePool(t)

	id := uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO businesses (id, name, logo_url) VALUES ($1, $2, $3)`,
		id, "Old Name", "old")
	require.NoError(t, err)

	// Writer A: profile editor loads the row while logo_url is still 'old'.
	snapshotA, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "old", snapshotA.LogoURL, "precondition: logo starts at 'old'")

	// Writer B: logo upload commits the new logo through the targeted path.
	require.NoError(t, repo.UpdateLogoURL(ctx, id, "new"))

	// Writer A: commits its name change from the stale snapshot. The targeted
	// profile Update omits logo_url, so A's stale 'old' logo snapshot is NOT
	// carried back into the row.
	snapshotA.Name = "New Name"
	require.NoError(t, repo.Update(ctx, snapshotA))

	final, err := repo.GetByID(ctx, id)
	require.NoError(t, err)

	require.Equalf(t, "new", final.LogoURL,
		"freshly uploaded logo was reverted to %q by a concurrent profile edit (lost update + dangling object)",
		final.LogoURL)
	require.Equal(t, "New Name", final.Name,
		"profile editor's name change must survive alongside the logo upload")
}

// TestBusinessRepository_Update_DoesNotTouchLogoURL asserts the profile Update
// emits no logo_url SET clause, so a profile edit carrying a stale logo_url
// snapshot can never revert a concurrent logo upload. Reverting the fix (adding
// `.Set("logo_url", …)` back to Update) reintroduces the clause and fails this.
func TestBusinessRepository_Update_DoesNotTouchLogoURL(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	capture := &sqlCapturePool{pgxPool: mock}
	repo := &businessRepository{pool: capture, sb: newStatementBuilder()}

	mock.ExpectExec(`UPDATE businesses`).WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	// sqlCapturePool records the SQL before delegating to the mock, so the
	// captured statement is available regardless of how the mock matches args.
	// The assertions below run against that captured SQL, not Update's return.
	_ = repo.Update(context.Background(), &domain.Business{
		ID:      uuid.New(),
		Name:    "Acme",
		LogoURL: "should-not-be-written",
	})

	require.NotEmpty(t, capture.execSQL, "Update must emit an UPDATE statement")
	require.NotContains(t, strings.ToLower(capture.execSQL), "logo_url",
		"profile Update must not emit a logo_url SET clause (it would revert a concurrent logo upload)")
	require.Contains(t, strings.ToLower(capture.execSQL), "name",
		"sanity: the profile Update should still write the name column")
}

// TestBusinessRepository_UpdateLogoURL_NotFound asserts the targeted logo write
// surfaces ErrBusinessNotFound for a missing or soft-deleted row.
func TestBusinessRepository_UpdateLogoURL_NotFound(t *testing.T) {
	ctx := context.Background()
	pool, repo := settingsRacePool(t)

	err := repo.UpdateLogoURL(ctx, uuid.New(), "x")
	require.ErrorIs(t, err, domain.ErrBusinessNotFound, "missing row must map to ErrBusinessNotFound")

	deleted := uuid.New()
	_, err = pool.Exec(ctx, `INSERT INTO businesses (id, name, deleted_at) VALUES ($1, $2, now())`, deleted, "Gone")
	require.NoError(t, err)

	err = repo.UpdateLogoURL(ctx, deleted, "x")
	require.ErrorIs(t, err, domain.ErrBusinessNotFound, "soft-deleted row must map to ErrBusinessNotFound")
}
