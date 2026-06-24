package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestCollapseSubject(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"tasks.telegram", "tasks.telegram"},
		{"tasks.vk", "tasks.vk"},
		{"_INBOX.abc123xyz", "_INBOX"},
		{"_INBOX.", "_INBOX"},
		{"", ""},
		{"_INBOX", "_INBOX"},
	}
	for _, tc := range tests {
		if got := CollapseSubject(tc.in); got != tc.want {
			t.Errorf("CollapseSubject(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRecordNATSPublish(t *testing.T) {
	before := testutil.ToFloat64(natsPublishTotal.WithLabelValues("tasks.telegram", "ok"))
	beforeInbox := testutil.ToFloat64(natsPublishTotal.WithLabelValues("_INBOX", "error"))

	RecordNATSPublish("tasks.telegram", "ok", 250*time.Microsecond)
	RecordNATSPublish("_INBOX.xyz", "error", 1*time.Millisecond)

	if got := testutil.ToFloat64(natsPublishTotal.WithLabelValues("tasks.telegram", "ok")); got != before+1 {
		t.Errorf("natsPublishTotal{tasks.telegram,ok} = %v, want %v", got, before+1)
	}
	if got := testutil.ToFloat64(natsPublishTotal.WithLabelValues("_INBOX", "error")); got != beforeInbox+1 {
		t.Errorf("natsPublishTotal{_INBOX,error} = %v, want %v (subject collapsed)", got, beforeInbox+1)
	}
}

func TestRecordNATSHandler(t *testing.T) {
	RecordNATSHandler("tasks.vk", "ok", 50*time.Millisecond)
	RecordNATSHandler("_INBOX.abc", "error", 5*time.Millisecond)
	if natsHandlerDuration == nil {
		t.Fatal("natsHandlerDuration not initialized")
	}
}

// TestIncA2AHandlerPanic_NormalizesUnknownAgent asserts a valid agent id keeps
// its own label series while any unexpected/hostile id collapses to "unknown"
// (never creating a raw label series) so a stray caller can't explode
// agent_id cardinality.
func TestIncA2AHandlerPanic_NormalizesUnknownAgent(t *testing.T) {
	const hostile = "'; DROP TABLE x; --"

	validBefore := testutil.ToFloat64(a2aHandlerPanicsTotal.WithLabelValues("telegram"))
	unknownBefore := testutil.ToFloat64(a2aHandlerPanicsTotal.WithLabelValues("unknown"))

	IncA2AHandlerPanic("telegram")
	IncA2AHandlerPanic(hostile)

	if got := testutil.ToFloat64(a2aHandlerPanicsTotal.WithLabelValues("telegram")); got != validBefore+1 {
		t.Errorf("a2aHandlerPanicsTotal{telegram} = %v, want %v (valid id keeps its own label)", got, validBefore+1)
	}
	if got := testutil.ToFloat64(a2aHandlerPanicsTotal.WithLabelValues("unknown")); got != unknownBefore+1 {
		t.Errorf("a2aHandlerPanicsTotal{unknown} = %v, want %v (hostile id collapses to unknown)", got, unknownBefore+1)
	}

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}
	mf := findMetric(families, "a2a_handler_panics_total")
	if mf == nil {
		t.Fatal("a2a_handler_panics_total metric family not found")
	}
	if findSample(mf, map[string]string{"agent_id": hostile}) != nil {
		t.Error("hostile agent_id must collapse to 'unknown', not create a raw label series")
	}
}
