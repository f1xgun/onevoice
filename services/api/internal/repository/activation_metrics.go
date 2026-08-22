// Package repository — activation_metrics.go.
//
// ActivationRepository derives the signup→connect activation funnel straight
// from the canonical Postgres records (users, businesses, integrations). It is
// read-only analytics — no new write path — so the funnel is computed from data
// the product already persists rather than a duplicate event stream, mirroring
// how PresenceRepository derives the North-Star from the Mongo `posts`
// collection.
package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// ActivationStats is one activation-funnel reading over a trailing window:
// Signups is the number of users who registered in the window (funnel top), and
// Activated the subset of those users that reached the connected state — owning
// at least one business with an active, non-deleted integration (funnel bottom).
// activation_rate = Activated / Signups.
type ActivationStats struct {
	Signups   int
	Activated int
}

// ActivationRepository computes the activation funnel from the PG pool.
type ActivationRepository struct {
	pool pgxPool
}

// NewActivationRepository constructs the activation-funnel repo over the pool.
func NewActivationRepository(pool pgxPool) *ActivationRepository {
	return &ActivationRepository{pool: pool}
}

// recentActivationQuery counts, over non-deleted users created at or after
// `since`, the total (signups) and the subset that own at least one non-deleted
// business with an active, non-deleted integration (activated). The active
// status literal is bound as $2 (domain.IntegrationStatusActive), never
// interpolated into the SQL.
const recentActivationQuery = `
SELECT
    COUNT(*) AS signups,
    COUNT(*) FILTER (WHERE EXISTS (
        SELECT 1
        FROM businesses b
        JOIN integrations i
          ON i.business_id = b.id
         AND i.deleted_at IS NULL
         AND i.status = $2
        WHERE b.user_id = u.id
          AND b.deleted_at IS NULL
    )) AS activated
FROM users u
WHERE u.deleted_at IS NULL
  AND u.created_at >= $1`

// RecentActivation returns the signup and activated counts for users created at
// or after `since`. COUNT always yields a row, so an empty window returns a
// zero-valued ActivationStats (no error).
func (r *ActivationRepository) RecentActivation(ctx context.Context, since time.Time) (ActivationStats, error) {
	var stats ActivationStats
	if err := r.pool.QueryRow(ctx, recentActivationQuery, since, domain.IntegrationStatusActive).
		Scan(&stats.Signups, &stats.Activated); err != nil {
		return ActivationStats{}, fmt.Errorf("query activation funnel: %w", err)
	}
	return stats, nil
}
