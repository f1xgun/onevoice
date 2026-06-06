package metrics

import (
	"testing"
	"time"

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
