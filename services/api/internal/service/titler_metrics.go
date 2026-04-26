package service

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// autoTitleAttempts counts auto-title generation attempts by terminal status
// and outcome. Every call to Titler.GenerateAndSave records exactly one
// (status, outcome) pair when it returns.
//
// Label values:
//
//	status:  "success" | "failure"
//	outcome: "ok" | "llm_error" | "empty_response" | "pii_reject" |
//	         "manual_won_race" | "persist_error" | "terminal_write_error"
//
// I-02 resolution: the in-progress sentinel label was deliberately dropped.
// Every GenerateAndSave call resolves to exactly one terminal status pair;
// a phantom in-progress label that is never incremented would pollute the
// Prometheus catalog without observability value. The auto title attempt
// volume is fully recoverable from the rate of incremented terminal labels.
var autoTitleAttempts = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "auto_title_attempts_total",
	Help: "Total auto-title generation attempts by status and outcome.",
}, []string{"status", "outcome"})

// recordAttempt increments the auto_title_attempts_total counter for the
// given (status, outcome) pair. Status is "success" or "failure"; outcome
// is the catalog of terminal outcomes documented on autoTitleAttempts.
func recordAttempt(status, outcome string) {
	autoTitleAttempts.WithLabelValues(status, outcome).Inc()
}
