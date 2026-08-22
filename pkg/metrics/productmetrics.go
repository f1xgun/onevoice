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

// Activation-funnel gauges. Like the North-Star gauges above, these are
// periodically recomputed by the productmetrics collector — here from the
// canonical Postgres records (users/businesses/integrations) — and Set to the
// current value. They carry no labels.
//
// The funnel is signup → connect over the trailing 7-day cohort:
// signups7d counts users who registered in the window; activatedSignups7d is the
// subset that reached the "connected" state (owns ≥1 business with an active
// integration); activationRate7d = activatedSignups7d / signups7d, a fraction in
// [0,1]. The two counts are exported alongside the ratio so a dashboard can show
// funnel volume, not just the derived conversion.
var (
	signups7d = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "signups_7d",
		Help: "Users who registered in the trailing 7 days (activation-funnel top).",
	})
	activatedSignups7d = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "activated_signups_7d",
		Help: "Trailing-7-day signups that reached the connected state (own ≥1 business with an active integration).",
	})
	activationRate7d = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "activation_rate_7d",
		Help: "Activation funnel: fraction of trailing-7-day signups that connected an integration (0 when there were no signups).",
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

// SetActivationFunnel publishes a freshly computed activation-funnel reading.
// signups is the number of users who registered in the trailing 7 days and
// activated the subset that reached the connected state. The rate is set to 0
// when there were no signups so the gauge never carries NaN.
func SetActivationFunnel(signups, activated int) {
	signups7d.Set(float64(signups))
	activatedSignups7d.Set(float64(activated))
	rate := 0.0
	if signups > 0 {
		rate = float64(activated) / float64(signups)
	}
	activationRate7d.Set(rate)
}
