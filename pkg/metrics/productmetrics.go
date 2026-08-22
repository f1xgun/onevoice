package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// North-Star gauges. Unlike the event counters in product.go (incremented at the
// moment something happens), these are periodically recomputed by the
// productmetrics collector from the durable per-business record (the Mongo
// `posts` collection) and Set to the current value. They carry no labels — each
// is a single aggregate number — so cardinality is fixed.
//
// The North-Star is "weekly AI-actioned presence updates per active business":
// nsmPresenceUpdatesPerActiveBiz = presenceUpdates7d / activeBusinesses7d, where
// an active business is one with ≥1 successful presence update in the trailing
// 7-day window. The two components are exported alongside the ratio so a
// dashboard can show volume and reach, not just the derived figure.
//
// See pkg/metrics/README.md for the label-cardinality convention.
var (
	activeBusinesses7d = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "active_businesses_7d",
		Help: "Businesses with ≥1 successful presence update in the trailing 7 days (North-Star denominator).",
	})
	presenceUpdates7d = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "presence_updates_7d",
		Help: "Successful AI-actioned presence updates in the trailing 7 days (North-Star numerator).",
	})
	nsmPresenceUpdatesPerActiveBiz = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "nsm_presence_updates_per_active_business_7d",
		Help: "North-Star: weekly successful presence updates per active business (0 when no business is active).",
	})
)

// SetNorthStar publishes a freshly computed North-Star reading. updates is the
// count of successful presence updates and activeBusinesses the number of
// distinct businesses that produced them, both over the trailing 7 days. The
// ratio is set to 0 when no business is active so the gauge never carries NaN.
func SetNorthStar(updates, activeBusinesses int) {
	presenceUpdates7d.Set(float64(updates))
	activeBusinesses7d.Set(float64(activeBusinesses))
	ratio := 0.0
	if activeBusinesses > 0 {
		ratio = float64(updates) / float64(activeBusinesses)
	}
	nsmPresenceUpdatesPerActiveBiz.Set(ratio)
}
