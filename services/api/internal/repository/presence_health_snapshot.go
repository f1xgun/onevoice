// Package repository — presence_health_snapshot.go
//
// Postgres-backed presence_health_snapshots store behind the weekly
// presence-health trend. One row per (business, ISO-week); the UNIQUE
// (business_id, iso_week) constraint plus the ON CONFLICT DO UPDATE upsert keep
// it at most one row per week (idempotent per week). EnumerateActiveBusinessIDs
// mirrors CreditGrantExtAdapter's active-business enumerator so the snapshot
// worker excludes soft-deleted businesses, exactly like the credit-grant fleet.

package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/f1xgun/onevoice/pkg/domain"
)

type presenceHealthSnapshotRepository struct {
	pool pgxPool
}

// NewPresenceHealthSnapshotRepository constructs the Postgres-backed
// PresenceHealthSnapshotRepository sharing the pool with the other repositories.
func NewPresenceHealthSnapshotRepository(pool pgxPool) domain.PresenceHealthSnapshotRepository {
	return &presenceHealthSnapshotRepository{pool: pool}
}

// presenceHealthSnapshotColumns is the canonical projection shared by every
// snapshot SELECT so the scan stays in lockstep with the column order.
const presenceHealthSnapshotColumns = `id, business_id, iso_week, composite,
	rating_score, sla_score, coverage_score, sync_score, created_at`

func scanPresenceHealthSnapshot(row scanner) (domain.PresenceHealthSnapshot, error) {
	var s domain.PresenceHealthSnapshot
	err := row.Scan(
		&s.ID,
		&s.BusinessID,
		&s.ISOWeek,
		&s.Composite,
		&s.RatingScore,
		&s.SLAScore,
		&s.CoverageScore,
		&s.SyncScore,
		&s.CreatedAt,
	)
	return s, err
}

// Upsert — see domain.PresenceHealthSnapshotRepository docstring. The ON
// CONFLICT (business_id, iso_week) DO UPDATE is what makes a second stamp in the
// same week overwrite rather than duplicate — the idempotency guard the
// fail-on-revert test exercises.
func (r *presenceHealthSnapshotRepository) Upsert(ctx context.Context, snap domain.PresenceHealthSnapshot) error {
	if snap.BusinessID == uuid.Nil {
		return fmt.Errorf("upsert presence health snapshot: business id is required")
	}
	if snap.ISOWeek == "" {
		return fmt.Errorf("upsert presence health snapshot: iso week is required")
	}
	if snap.ID == uuid.Nil {
		snap.ID = uuid.New()
	}
	const q = `
		INSERT INTO presence_health_snapshots
			(id, business_id, iso_week, composite, rating_score, sla_score, coverage_score, sync_score)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (business_id, iso_week) DO UPDATE SET
			composite      = EXCLUDED.composite,
			rating_score   = EXCLUDED.rating_score,
			sla_score      = EXCLUDED.sla_score,
			coverage_score = EXCLUDED.coverage_score,
			sync_score     = EXCLUDED.sync_score`
	if _, err := r.pool.Exec(ctx, q,
		snap.ID, snap.BusinessID, snap.ISOWeek, snap.Composite,
		snap.RatingScore, snap.SLAScore, snap.CoverageScore, snap.SyncScore,
	); err != nil {
		return fmt.Errorf("upsert presence health snapshot: %w", err)
	}
	return nil
}

// GetMostRecentPrior — see domain.PresenceHealthSnapshotRepository docstring.
// The `iso_week < $2` predicate excludes the current week (and every later
// week), so a lazy same-week upsert can never become its own trend baseline;
// ORDER BY iso_week DESC LIMIT 1 takes the newest strictly-prior week.
func (r *presenceHealthSnapshotRepository) GetMostRecentPrior(ctx context.Context, businessID uuid.UUID, currentWeek string) (*domain.PresenceHealthSnapshot, error) {
	if businessID == uuid.Nil {
		return nil, fmt.Errorf("get prior presence health snapshot: business id is required")
	}
	q := `SELECT ` + presenceHealthSnapshotColumns + `
		FROM presence_health_snapshots
		WHERE business_id = $1 AND iso_week < $2
		ORDER BY iso_week DESC
		LIMIT 1`
	row := r.pool.QueryRow(ctx, q, businessID, currentWeek)
	snap, err := scanPresenceHealthSnapshot(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get prior presence health snapshot: %w", err)
	}
	return &snap, nil
}

// EnumerateActiveBusinessIDs returns the id of every non-soft-deleted business —
// the fleet the weekly snapshot worker stamps. It mirrors
// CreditGrantExtAdapter.EnumerateActiveBusinessIDs so both background workers
// iterate the identical active set (soft-deleted businesses excluded).
func (r *presenceHealthSnapshotRepository) EnumerateActiveBusinessIDs(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := r.pool.Query(ctx, `SELECT id FROM businesses WHERE deleted_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("EnumerateActiveBusinessIDs: query: %w", err)
	}
	ids, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (uuid.UUID, error) {
		var id uuid.UUID
		err := row.Scan(&id)
		return id, err
	})
	if err != nil {
		return nil, fmt.Errorf("EnumerateActiveBusinessIDs: collect: %w", err)
	}
	return ids, nil
}
