// Package productmetrics periodically recomputes the North-Star gauges from the
// product's durable per-business record (the Mongo `posts` collection) and
// publishes them to Prometheus. It adds no write path: the North-Star is derived
// from data already persisted when a publishing tool call succeeds.
package productmetrics

import (
	"context"
	"log/slog"
	"time"

	"github.com/f1xgun/onevoice/pkg/metrics"
	"github.com/f1xgun/onevoice/services/api/internal/repository"
)

// northStarWindow is the trailing window for the weekly North-Star.
const northStarWindow = 7 * 24 * time.Hour

// presenceSource is the read surface the collector needs;
// *repository.PresenceRepository satisfies it. Injected so the collector is
// unit-testable without Mongo.
type presenceSource interface {
	RecentPresence(ctx context.Context, since time.Time) (repository.PresenceStats, error)
}

// Collector recomputes the North-Star gauges on an interval.
type Collector struct {
	src      presenceSource
	interval time.Duration
	log      *slog.Logger
}

// NewCollector builds a Collector. interval <= 0 disables the ticker (Start then
// runs a single collection and returns). A nil src makes Collect a no-op.
func NewCollector(src presenceSource, interval time.Duration, log *slog.Logger) *Collector {
	if log == nil {
		log = slog.Default()
	}
	return &Collector{src: src, interval: interval, log: log}
}

// Start collects once immediately, then repeats on the configured interval until
// ctx is done. Mirrors ReviewSyncer.Start.
func (c *Collector) Start(ctx context.Context) {
	c.Collect(ctx)
	if c.interval <= 0 {
		return
	}
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.Collect(ctx)
		case <-ctx.Done():
			return
		}
	}
}

// Collect recomputes the North-Star over the trailing window and publishes the
// gauges. Best-effort: a query failure is logged and leaves the last-published
// gauge values in place (a stale reading beats a zeroed one). Uses time.Now to
// anchor the window; runs off the request path.
func (c *Collector) Collect(ctx context.Context) {
	if c.src == nil {
		return
	}
	since := time.Now().Add(-northStarWindow)
	stats, err := c.src.RecentPresence(ctx, since)
	if err != nil {
		c.log.WarnContext(ctx, "product metrics: north-star collection failed, keeping last values", "error", err)
		return
	}
	metrics.SetNorthStar(stats.Updates, stats.ActiveBusinesses)
}
