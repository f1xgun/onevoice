package repository

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// settingsRacePool opens a pgxpool against TEST_POSTGRES_URL, creating an
// isolated per-test schema that owns a minimal businesses table. The schema is
// self-contained (it does not depend on the shared DB's migration state) and
// is dropped on cleanup. The pool's search_path is pinned to the test schema so
// the repository's unqualified `businesses` references resolve there.
func settingsRacePool(t *testing.T) (*pgxpool.Pool, *businessRepository) {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_URL not set; skipping business settings race test")
	}

	ctx := context.Background()
	schema := "biz_settings_race_" + uuid.NewString()[:8]

	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", schema))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schema))
		pool.Close()
	})

	_, err = pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE %s.businesses (
		id uuid PRIMARY KEY,
		name text NOT NULL DEFAULT '',
		category text NOT NULL DEFAULT '',
		address text NOT NULL DEFAULT '',
		phone text NOT NULL DEFAULT '',
		website text,
		description text NOT NULL DEFAULT '',
		logo_url text NOT NULL DEFAULT '',
		settings jsonb NOT NULL DEFAULT '{}'::jsonb,
		deleted_at timestamptz,
		deletion_requested_at timestamptz,
		deletion_canceled_at timestamptz,
		created_at timestamptz NOT NULL DEFAULT now(),
		updated_at timestamptz NOT NULL DEFAULT now()
	)`, schema))
	require.NoError(t, err)

	repo := &businessRepository{pool: pool, sb: newStatementBuilder()}
	return pool, repo
}

// TestBusinessRepository_SettingsSubKeyWriters_NoLostUpdate is the fail-on-revert
// guard for the settings lost-update bug.
//
// Two writers touch DIFFERENT settings sub-keys from snapshots taken before the
// other committed: writer A tightens tool_approvals[X] from auto→manual (a HITL
// floor the owner just gated behind human approval), while writer B — which
// loaded the row BEFORE A's change — writes a schedule edit. With the old
// whole-map read-modify-write (GetByID → mutate one sub-key → write the WHOLE
// settings map back) B's stale snapshot re-persists tool_approvals[X]=auto and
// silently reverts A's gate. With the targeted jsonb_set writes each statement
// touches only the key it owns, so both survive.
func TestBusinessRepository_SettingsSubKeyWriters_NoLostUpdate(t *testing.T) {
	const gatedTool = "telegram__send_channel_post"

	seed := func(t *testing.T) (context.Context, *pgxpool.Pool, *businessRepository, uuid.UUID) {
		t.Helper()
		ctx := context.Background()
		pool, repo := settingsRacePool(t)
		id := uuid.New()
		_, err := pool.Exec(ctx, `INSERT INTO businesses (id, name, settings) VALUES ($1, $2, $3::jsonb)`,
			id, "Acme", `{"tool_approvals":{"`+gatedTool+`":"auto"},"schedule":{"mon":"closed"}}`)
		require.NoError(t, err)
		return ctx, pool, repo, id
	}

	t.Run("targeted jsonb_set keeps both sub-key writes", func(t *testing.T) {
		ctx, _, repo, id := seed(t)

		// Writer B captures a snapshot of settings BEFORE writer A tightens the
		// floor — two concurrent editors that each GetByID at their own T0.
		snapshotB, err := repo.GetByID(ctx, id)
		require.NoError(t, err)
		require.Equal(t, domain.ToolFloorAuto, snapshotB.ToolApprovals()[gatedTool],
			"precondition: gated tool starts at auto")

		// Writer A: tighten the per-tool HITL floor to manual.
		require.NoError(t, repo.UpdateToolApprovals(ctx, id, map[string]domain.ToolFloor{
			gatedTool: domain.ToolFloorManual,
		}))

		// Writer B: persist its schedule edit through the targeted path. It owns
		// only the schedule sub-key, so its stale tool_approvals snapshot is not
		// carried back into the row.
		require.NoError(t, repo.UpdateSettingsKeys(ctx, id, map[string]interface{}{
			"schedule": map[string]string{"mon": "09:00-21:00"},
		}))

		final, err := repo.GetByID(ctx, id)
		require.NoError(t, err)

		gotFloor := final.ToolApprovals()[gatedTool]
		require.Equalf(t, domain.ToolFloorManual, gotFloor,
			"tightened HITL floor for %s was reverted to %q by a concurrent schedule write (lost update)",
			gatedTool, gotFloor)

		sched, ok := final.Settings["schedule"].(map[string]interface{})
		require.True(t, ok, "writer B's schedule sub-key must survive")
		require.Equal(t, "09:00-21:00", sched["mon"],
			"writer B's schedule edit must survive alongside writer A's approval change")
	})

	// fail-on-revert anchor: drives the legacy whole-map read-modify-write that
	// the fix removed (GetByID at T0 → mutate one sub-key in the in-memory map →
	// write the WHOLE settings map back). It must REVERT writer A's tightened
	// floor — proving the targeted jsonb_set in the subtest above is what closes
	// the hole. If this ever stops reverting, the legacy hazard is gone and this
	// anchor can be deleted.
	t.Run("legacy whole-map write reverts the tightened floor", func(t *testing.T) {
		ctx, _, repo, id := seed(t)

		snapshotB, err := repo.GetByID(ctx, id)
		require.NoError(t, err)

		require.NoError(t, repo.UpdateToolApprovals(ctx, id, map[string]domain.ToolFloor{
			gatedTool: domain.ToolFloorManual,
		}))

		// Writer B mutates its schedule key in the STALE snapshot and writes the
		// whole map back — exactly the pre-fix repo.Update(business) behavior.
		snapshotB.Settings["schedule"] = map[string]string{"mon": "09:00-21:00"}
		_, err = repo.pool.Exec(ctx,
			`UPDATE businesses SET settings = $2, updated_at = NOW() WHERE id = $1`,
			id, snapshotB.Settings)
		require.NoError(t, err)

		reverted, err := repo.GetByID(ctx, id)
		require.NoError(t, err)
		require.Equal(t, domain.ToolFloorAuto, reverted.ToolApprovals()[gatedTool],
			"legacy whole-map write must silently revert the tightened floor — this is the bug the fix removes")
	})
}

// TestBusinessRepository_VoiceProfileWrite_PreservesSiblingKeys proves the
// voiceProfile settings write goes through the same targeted jsonb_set path and
// therefore cannot clobber a concurrently-written sibling key. Writer A tightens
// a HITL floor from a snapshot taken before writer B set the voiceProfile; both
// survive because each statement touches only the sub-key it owns.
func TestBusinessRepository_VoiceProfileWrite_PreservesSiblingKeys(t *testing.T) {
	const gatedTool = "telegram__send_channel_post"
	ctx := context.Background()
	pool, repo := settingsRacePool(t)

	id := uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO businesses (id, name, settings) VALUES ($1, $2, $3::jsonb)`,
		id, "Acme", `{"tool_approvals":{"`+gatedTool+`":"auto"},"descriptionTemplate":"{name}"}`)
	require.NoError(t, err)

	require.NoError(t, repo.UpdateSettingsKeys(ctx, id, map[string]interface{}{
		"voiceProfile": "Пиши тепло, без эмодзи.",
	}))
	require.NoError(t, repo.UpdateToolApprovals(ctx, id, map[string]domain.ToolFloor{
		gatedTool: domain.ToolFloorManual,
	}))

	final, err := repo.GetByID(ctx, id)
	require.NoError(t, err)

	require.Equal(t, "Пиши тепло, без эмодзи.", final.Settings["voiceProfile"],
		"voiceProfile write must persist")
	require.Equal(t, domain.ToolFloorManual, final.ToolApprovals()[gatedTool],
		"a concurrent voiceProfile write must not revert the tightened HITL floor")
	require.Equal(t, "{name}", final.Settings["descriptionTemplate"],
		"the pre-existing descriptionTemplate sibling key must survive a voiceProfile write")
}

// TestBusinessRepository_Update_DoesNotTouchSettings asserts the generic profile
// Update never rewrites the settings JSONB, so a profile/logo edit carrying a
// stale settings snapshot cannot revert a concurrent settings sub-key change.
func TestBusinessRepository_Update_DoesNotTouchSettings(t *testing.T) {
	ctx := context.Background()
	pool, repo := settingsRacePool(t)

	const gatedTool = "vk__publish_post"
	id := uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO businesses (id, name, settings) VALUES ($1, $2, $3::jsonb)`,
		id, "Old Name", `{"tool_approvals":{"`+gatedTool+`":"auto"}}`)
	require.NoError(t, err)

	// A profile editor loaded the row while the gate was still auto.
	staleProfile, err := repo.GetByID(ctx, id)
	require.NoError(t, err)

	// Owner tightens the gate to manual.
	require.NoError(t, repo.UpdateToolApprovals(ctx, id, map[string]domain.ToolFloor{
		gatedTool: domain.ToolFloorManual,
	}))

	// The profile editor commits its name change from the stale snapshot.
	staleProfile.Name = "New Name"
	require.NoError(t, repo.Update(ctx, staleProfile))

	final, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "New Name", final.Name, "profile column edit must persist")
	require.Equal(t, domain.ToolFloorManual, final.ToolApprovals()[gatedTool],
		"profile Update must not revert the tightened HITL floor")
}

// TestBusinessRepository_UpdateSettingsKeys_NotFound asserts the targeted
// settings write surfaces ErrBusinessNotFound for a missing or soft-deleted row.
func TestBusinessRepository_UpdateSettingsKeys_NotFound(t *testing.T) {
	ctx := context.Background()
	pool, repo := settingsRacePool(t)

	err := repo.UpdateSettingsKeys(ctx, uuid.New(), map[string]interface{}{"schedule": "x"})
	require.ErrorIs(t, err, domain.ErrBusinessNotFound, "missing row must map to ErrBusinessNotFound")

	deleted := uuid.New()
	_, err = pool.Exec(ctx, `INSERT INTO businesses (id, name, deleted_at) VALUES ($1, $2, now())`, deleted, "Gone")
	require.NoError(t, err)

	err = repo.UpdateSettingsKeys(ctx, deleted, map[string]interface{}{"schedule": "x"})
	require.ErrorIs(t, err, domain.ErrBusinessNotFound, "soft-deleted row must map to ErrBusinessNotFound")

	err = repo.UpdateToolApprovals(ctx, deleted, map[string]domain.ToolFloor{"t": domain.ToolFloorManual})
	require.ErrorIs(t, err, domain.ErrBusinessNotFound, "soft-deleted row must map to ErrBusinessNotFound")
}
