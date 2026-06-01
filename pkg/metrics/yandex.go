package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// BrowserPoolContexts is the live count of Playwright BrowserContexts held by
// the Yandex.Business agent's BrowserPool. Each context is one Chromium
// browsing session (~80–150 MB) so this gauge tracks the pool's memory
// pressure proxy directly.
var BrowserPoolContexts = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "browserpool_contexts",
	Help: "Current number of live Playwright BrowserContexts in the pool.",
})

// BrowserPoolEvictions counts BrowserContext evictions by reason.
//
//	reason="lru"  — cap-driven eviction at acquire-time when the pool is full.
//	reason="idle" — stale-eviction by the background idle sweep.
var BrowserPoolEvictions = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "browserpool_evictions_total",
	Help: "BrowserContext evictions by reason (lru = cap-driven; idle = stale).",
}, []string{"reason"})

// RPAStepDuration records Yandex.Business RPA helper step duration.
// step ∈ {listCompanies, getInfo, getReviews, replyReview, createPost,
// updateHours, updateInfo, uploadPhoto} — bounded set, hard-coded at the
// call site. result ∈ {ok, error}. See pkg/metrics/README.md for the
// cardinality allowlist; never derive `step` from runtime variables.
var RPAStepDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "rpa_step_duration_seconds",
	Help:    "Yandex.Business RPA step duration in seconds.",
	Buckets: []float64{0.1, 0.5, 1, 2.5, 5, 10, 30, 60},
}, []string{"step", "result"})
