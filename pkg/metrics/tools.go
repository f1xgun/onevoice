package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	toolDispatchTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "tool_dispatch_total",
		Help: "Total number of tool dispatches.",
	}, []string{"tool", "agent", "status"})

	toolDispatchDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "tool_dispatch_duration_seconds",
		Help:    "Tool dispatch duration in seconds.",
		Buckets: []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
	}, []string{"tool", "agent"})
)

// RecordToolDispatch records a completed tool dispatch.
func RecordToolDispatch(tool, agent, status string, duration time.Duration) {
	toolDispatchTotal.WithLabelValues(tool, agent, status).Inc()
	toolDispatchDuration.WithLabelValues(tool, agent).Observe(duration.Seconds())
}
