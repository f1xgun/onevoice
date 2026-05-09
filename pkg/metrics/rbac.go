package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var rbacCheckTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "rbac_check_total",
	Help: "Total number of authz.Can() permission evaluations.",
}, []string{"result"})

// IncRBACCheck records one Can() evaluation. result is one of "allow" | "deny" | "missing".
func IncRBACCheck(result string) {
	rbacCheckTotal.WithLabelValues(result).Inc()
}

// GetRBACCheckCounter returns the underlying CounterVec so integration tests
// (Plan 02-07 LOW #9) can call prometheus/testutil.ToFloat64 on it directly
// to assert rbac_check_total{result} increased by a given amount. Not for
// production use.
func GetRBACCheckCounter() *prometheus.CounterVec {
	return rbacCheckTotal
}
