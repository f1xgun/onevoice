package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestSetRateLimiterEnabled(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
		want    float64
	}{
		{name: "enabled", enabled: true, want: 1},
		{name: "disabled", enabled: false, want: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			SetRateLimiterEnabled(tc.enabled)

			families, err := prometheus.DefaultGatherer.Gather()
			if err != nil {
				t.Fatalf("gather: %v", err)
			}
			mf := findMetric(families, "rate_limiter_enabled")
			if mf == nil {
				t.Fatal("rate_limiter_enabled gauge not found")
			}
			sample := findSample(mf, nil)
			if sample == nil {
				t.Fatal("rate_limiter_enabled sample not found")
			}
			if got := sample.GetGauge().GetValue(); got != tc.want {
				t.Errorf("rate_limiter_enabled = %v, want %v", got, tc.want)
			}
		})
	}
}
