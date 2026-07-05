package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// PresenceHealthSnapshot is one weekly stamp of a business's composite
// presence-health score. One row per (business, ISO-week) — the UNIQUE
// (business_id, iso_week) constraint plus an upsert keeps it at most one per
// week. The trend endpoint reads the most-recent PRIOR-week snapshot to derive
// the week-over-week delta.
//
// Every field is an aggregate number: no author names, review text, or reply
// text is ever stored here, so the snapshot table carries no personal data.
// SyncScore is nullable — a business with no connected-channel sync signal
// drops the sync dimension (the other three weights renormalize over 0.90), and
// that absence is preserved as NULL rather than a misleading zero.
type PresenceHealthSnapshot struct {
	ID            uuid.UUID `db:"id"`
	BusinessID    uuid.UUID `db:"business_id"`
	ISOWeek       string    `db:"iso_week"`
	Composite     int       `db:"composite"`
	RatingScore   int       `db:"rating_score"`
	SLAScore      int       `db:"sla_score"`
	CoverageScore int       `db:"coverage_score"`
	SyncScore     *int      `db:"sync_score"`
	CreatedAt     time.Time `db:"created_at"`
}

// PresenceHealthSnapshotRepository persists the weekly presence-health snapshot
// used for the week-over-week trend. See docs/pkg/domain-repository.md.
type PresenceHealthSnapshotRepository interface {
	// Upsert stamps a business's snapshot for one ISO-week, keyed on the UNIQUE
	// (business_id, iso_week). A second stamp in the same week updates the
	// existing row rather than inserting a duplicate, so the table holds at most
	// one row per business per week (idempotent per week).
	Upsert(ctx context.Context, snap PresenceHealthSnapshot) error

	// GetMostRecentPrior returns the newest snapshot for a business strictly
	// BEFORE currentWeek (ISO-week string, lexically comparable as 'YYYY-Www').
	// It returns (nil, nil) when no prior-week snapshot exists so the trend delta
	// reads as null on the first-ever week. The current week's own snapshot is
	// excluded so a lazy same-week upsert never becomes its own baseline.
	GetMostRecentPrior(ctx context.Context, businessID uuid.UUID, currentWeek string) (*PresenceHealthSnapshot, error)

	// EnumerateActiveBusinessIDs returns the id of every non-soft-deleted
	// business — the fleet the weekly snapshot worker stamps.
	EnumerateActiveBusinessIDs(ctx context.Context) ([]uuid.UUID, error)
}
