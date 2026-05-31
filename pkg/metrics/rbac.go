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

// GetRBACCheckCounter exposes the underlying CounterVec for prometheus/testutil
// assertions. Not for production use.
func GetRBACCheckCounter() *prometheus.CounterVec {
	return rbacCheckTotal
}
