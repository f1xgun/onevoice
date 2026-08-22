// Package productmetrics periodically recomputes the product gauges — the
// North-Star and the activation funnel — from the product's durable records and
// publishes them to Prometheus. It adds no write path: the North-Star is derived
// from the Mongo `posts` collection and the funnel from the Postgres
// users/businesses/integrations tables, all data already persisted on the
// request path.
package productmetrics

import (
	"context"
	"log/slog"
	"time"

	"github.com/f1xgun/onevoice/pkg/metrics"
	"github.com/f1xgun/onevoice/services/api/internal/repository"
)

// northStarWindow is the trailing window for both the North-Star and the
// activation funnel.
const northStarWindow = 7 * 24 * time.Hour

// PresenceSource is the North-Star read surface (Mongo `posts`);
// *repository.PresenceRepository satisfies it. Injected so the collector is
// unit-testable without Mongo.
type PresenceSource interface {
	RecentPresence(ctx context.Context, since time.Time) (repository.PresenceStats, error)
}

// ActivationSource is the activation-funnel read surface (Postgres);
// *repository.ActivationRepository satisfies it. Injected so the collector is
// unit-testable without Postgres.
type ActivationSource interface {
	RecentActivation(ctx context.Context, since time.Time) (repository.ActivationStats, error)
}

// Collector recomputes the product gauges on an interval. Each source is
// optional: a nil source leaves its gauges untouched, so the collector runs
// with whichever of Mongo / Postgres is available.
type Collector struct {
	presence   PresenceSource
	activation ActivationSource
	interval   time.Duration
	log        *slog.Logger
}

// NewCollector builds a Collector. interval <= 0 disables the ticker (Start then
// runs a single collection and returns). A nil source makes its half of Collect
// a no-op.
func NewCollector(presence PresenceSource, activation ActivationSource, interval time.Duration, log *slog.Logger) *Collector {
	if log == nil {
		log = slog.Default()
	}
	return &Collector{presence: presence, activation: activation, interval: interval, log: log}
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

// Collect recomputes both the North-Star and the activation funnel over the
// trailing window and publishes the gauges. Both halves are best-effort and
// independent: a failure in one is logged and leaves that half's last-published
// gauge values in place (a stale reading beats a zeroed one) without affecting
// the other. Uses time.Now to anchor the window; runs off the request path.
func (c *Collector) Collect(ctx context.Context) {
	since := time.Now().Add(-northStarWindow)
	c.collectNorthStar(ctx, since)
	c.collectActivation(ctx, since)
}

func (c *Collector) collectNorthStar(ctx context.Context, since time.Time) {
	if c.presence == nil {
		return
	}
	stats, err := c.presence.RecentPresence(ctx, since)
	if err != nil {
		c.log.WarnContext(ctx, "product metrics: north-star collection failed, keeping last values", "error", err)
		return
	}
	metrics.SetNorthStar(stats.Updates, stats.ActiveBusinesses)
}

func (c *Collector) collectActivation(ctx context.Context, since time.Time) {
	if c.activation == nil {
		return
	}
	stats, err := c.activation.RecentActivation(ctx, since)
	if err != nil {
		c.log.WarnContext(ctx, "product metrics: activation funnel collection failed, keeping last values", "error", err)
		return
	}
	metrics.SetActivationFunnel(stats.Signups, stats.Activated)
}
