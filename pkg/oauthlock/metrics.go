package oauthlock

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// ContendedTotal counts OAuth refresh attempts that failed to acquire the
// advisory lock and returned ErrLockBusy.
var ContendedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "oauth_refresh_lock_contended_total",
	Help: "OAuth refresh attempts that failed to acquire the advisory lock and returned ErrLockBusy.",
}, []string{"platform"})

// ExhaustedTotal counts OAuth refresh attempts that exhausted all backoff
// retries without acquiring the advisory lock.
var ExhaustedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "oauth_refresh_lock_exhausted_total",
	Help: "OAuth refresh attempts that exhausted all backoff retries without acquiring the advisory lock.",
}, []string{"platform"})

// RegisterMetrics is a no-op for the promauto-registered counters; it exists
// so tests can call it to verify the package is importable.
func RegisterMetrics(_ interface{}) {}
