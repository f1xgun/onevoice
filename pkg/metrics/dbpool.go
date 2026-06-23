package metrics

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.mongodb.org/mongo-driver/v2/event"
)

// DB pool collectors emit pgxpool + mongo pool RED signals.
//
// Cardinality budget — see pkg/metrics/README.md.
// Allowed labels: op ∈ {find,insert,update,delete,aggregate,count,findAndModify,other},
// result ∈ {ok,error}. Banlist: collection, database, query, business_id,
// user_id, request_id.
//
// NOTE: pgxpool.Pool.Stat() exposes cumulative aggregates only.
// acquire_duration is therefore a cumulative counter (suffix _total);
// dashboards derive mean via
//
//	rate(pgxpool_acquire_duration_seconds_total[5m])
//	  / rate(pgxpool_acquire_count_total[5m])
//
// A real histogram would require pgx Before/AfterAcquire hooks — out of
// scope here.

// PGXPoolCollector implements prometheus.Collector over a pgxpool.Pool.Stat().
// Stat() is a cheap point-in-time snapshot; Collect is safe to call on scrape.
type PGXPoolCollector struct {
	pool *pgxpool.Pool

	acquireCountDesc         *prometheus.Desc
	acquireDurationDesc      *prometheus.Desc
	acquiredConnsDesc        *prometheus.Desc
	idleConnsDesc            *prometheus.Desc
	maxConnsDesc             *prometheus.Desc
	canceledAcquireCountDesc *prometheus.Desc
	emptyAcquireCountDesc    *prometheus.Desc
	newConnsCountDesc        *prometheus.Desc
}

// NewPGXPoolCollector constructs a collector bound to a single pgxpool.Pool.
// The pool may be nil — Describe still ships the full descriptor set so
// registration succeeds — but Collect MUST NOT be invoked with a nil pool.
func NewPGXPoolCollector(pool *pgxpool.Pool) *PGXPoolCollector {
	return &PGXPoolCollector{
		pool:                     pool,
		acquireCountDesc:         prometheus.NewDesc("pgxpool_acquire_count_total", "Cumulative successful acquires.", nil, nil),
		acquireDurationDesc:      prometheus.NewDesc("pgxpool_acquire_duration_seconds_total", "Cumulative duration of successful acquires in seconds.", nil, nil),
		acquiredConnsDesc:        prometheus.NewDesc("pgxpool_acquired_conns", "Currently acquired connections.", nil, nil),
		idleConnsDesc:            prometheus.NewDesc("pgxpool_idle_conns", "Currently idle connections.", nil, nil),
		maxConnsDesc:             prometheus.NewDesc("pgxpool_max_conns", "Configured max connections.", nil, nil),
		canceledAcquireCountDesc: prometheus.NewDesc("pgxpool_canceled_acquire_count_total", "Cumulative canceled acquires (ctx cancel).", nil, nil),
		emptyAcquireCountDesc:    prometheus.NewDesc("pgxpool_empty_acquire_count_total", "Cumulative acquires that waited on an empty pool.", nil, nil),
		newConnsCountDesc:        prometheus.NewDesc("pgxpool_new_conns_count_total", "Cumulative new connections created.", nil, nil),
	}
}

// Describe emits all eight descriptors for prometheus.Register.
func (c *PGXPoolCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.acquireCountDesc
	ch <- c.acquireDurationDesc
	ch <- c.acquiredConnsDesc
	ch <- c.idleConnsDesc
	ch <- c.maxConnsDesc
	ch <- c.canceledAcquireCountDesc
	ch <- c.emptyAcquireCountDesc
	ch <- c.newConnsCountDesc
}

// Collect reads pgxpool.Pool.Stat() once and emits eight metric samples.
func (c *PGXPoolCollector) Collect(ch chan<- prometheus.Metric) {
	s := c.pool.Stat()
	ch <- prometheus.MustNewConstMetric(c.acquireCountDesc, prometheus.CounterValue, float64(s.AcquireCount()))
	ch <- prometheus.MustNewConstMetric(c.acquireDurationDesc, prometheus.CounterValue, s.AcquireDuration().Seconds())
	ch <- prometheus.MustNewConstMetric(c.acquiredConnsDesc, prometheus.GaugeValue, float64(s.AcquiredConns()))
	ch <- prometheus.MustNewConstMetric(c.idleConnsDesc, prometheus.GaugeValue, float64(s.IdleConns()))
	ch <- prometheus.MustNewConstMetric(c.maxConnsDesc, prometheus.GaugeValue, float64(s.MaxConns()))
	ch <- prometheus.MustNewConstMetric(c.canceledAcquireCountDesc, prometheus.CounterValue, float64(s.CanceledAcquireCount()))
	ch <- prometheus.MustNewConstMetric(c.emptyAcquireCountDesc, prometheus.CounterValue, float64(s.EmptyAcquireCount()))
	ch <- prometheus.MustNewConstMetric(c.newConnsCountDesc, prometheus.CounterValue, float64(s.NewConnsCount()))
}

var (
	mongoPoolInUse = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "mongo_pool_in_use",
		Help: "Currently checked-out mongo connections.",
	})

	mongoPoolCheckoutDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "mongo_pool_checkout_duration_seconds",
		Help:    "Time from checkout-started to checked-out in seconds.",
		Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.5, 1, 5},
	})

	mongoOpDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "mongo_op_duration_seconds",
		Help:    "Mongo command duration by op and result.",
		Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 5},
	}, []string{"op", "result"})
)

// checkoutStartTimes tracks per-ConnectionID checkout-start timestamps so the
// PoolMonitor can observe the duration between ConnectionCheckOutStarted and
// ConnectionCheckedOut. Map churn is bounded — entries are removed in both
// the success (ConnectionCheckedOut) and failure (ConnectionCheckOutFailed)
// branches via LoadAndDelete / Delete.
var checkoutStartTimes sync.Map // map[int64]time.Time keyed by event.PoolEvent.ConnectionID

// NewMongoPoolMonitor returns an *event.PoolMonitor whose single Event
// callback emits mongo_pool_checkout_duration_seconds + mongo_pool_in_use.
func NewMongoPoolMonitor() *event.PoolMonitor {
	return &event.PoolMonitor{
		Event: func(e *event.PoolEvent) {
			switch e.Type {
			case event.ConnectionCheckOutStarted:
				checkoutStartTimes.Store(e.ConnectionID, time.Now())
			case event.ConnectionCheckedOut:
				if v, ok := checkoutStartTimes.LoadAndDelete(e.ConnectionID); ok {
					if start, ok2 := v.(time.Time); ok2 {
						mongoPoolCheckoutDuration.Observe(time.Since(start).Seconds())
					}
				}
				mongoPoolInUse.Inc()
			case event.ConnectionCheckOutFailed:
				checkoutStartTimes.Delete(e.ConnectionID)
			case event.ConnectionCheckedIn:
				mongoPoolInUse.Dec()
			}
		},
	}
}

// labelOther is the shared catch-all label value for whitelist-collapse
// counters (unknown mongo op, unknown HITL decision). Centralized so the
// bounded "other" bucket is one constant rather than a repeated literal.
const labelOther = "other"

// allowedMongoOps caps the `op` label cardinality to a fixed, documented
// allowlist. Unknown ops collapse to "other" in normalizeMongoOp.
var allowedMongoOps = map[string]struct{}{
	"find":          {},
	"insert":        {},
	"update":        {},
	"delete":        {},
	"aggregate":     {},
	"count":         {},
	"findAndModify": {},
}

func normalizeMongoOp(op string) string {
	if _, ok := allowedMongoOps[op]; ok {
		return op
	}
	return labelOther
}

// NewMongoCommandMonitor returns an *event.CommandMonitor that observes
// mongo_op_duration_seconds{op,result} on every Succeeded / Failed callback.
func NewMongoCommandMonitor() *event.CommandMonitor {
	return &event.CommandMonitor{
		Succeeded: func(_ context.Context, e *event.CommandSucceededEvent) {
			mongoOpDuration.WithLabelValues(normalizeMongoOp(e.CommandName), "ok").Observe(e.Duration.Seconds())
		},
		Failed: func(_ context.Context, e *event.CommandFailedEvent) {
			mongoOpDuration.WithLabelValues(normalizeMongoOp(e.CommandName), "error").Observe(e.Duration.Seconds())
		},
	}
}
