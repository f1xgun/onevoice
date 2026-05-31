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
