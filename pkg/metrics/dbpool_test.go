package metrics

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"go.mongodb.org/mongo-driver/v2/event"
)

func TestPGXPoolCollector_RegistersAndDescribes(t *testing.T) {
	c := NewPGXPoolCollector(nil)
	if c == nil {
		t.Fatal("NewPGXPoolCollector returned nil")
	}
	ch := make(chan *prometheus.Desc, 16)
	c.Describe(ch)
	close(ch)
	count := 0
	for range ch {
		count++
	}
	if count != 8 {
		t.Errorf("Describe shipped %d descriptors, want 8", count)
	}
}

func TestNormalizeMongoOp(t *testing.T) {
	for _, op := range []string{"find", "insert", "update", "delete", "aggregate", "count", "findAndModify"} {
		if got := normalizeMongoOp(op); got != op {
			t.Errorf("normalizeMongoOp(%q) = %q, want %q", op, got, op)
		}
	}
	if got := normalizeMongoOp("listCollections"); got != "other" {
		t.Errorf("normalizeMongoOp(unknown) = %q, want other", got)
	}
}

func TestMongoMonitors_PoolEventLifecycle(t *testing.T) {
	before := testutil.ToFloat64(mongoPoolInUse)

	mon := NewMongoPoolMonitor()
	if mon == nil || mon.Event == nil {
		t.Fatal("NewMongoPoolMonitor returned monitor with nil Event")
	}
	mon.Event(&event.PoolEvent{Type: event.ConnectionCheckOutStarted, ConnectionID: 1})
	time.Sleep(2 * time.Millisecond)
	mon.Event(&event.PoolEvent{Type: event.ConnectionCheckedOut, ConnectionID: 1})
	mon.Event(&event.PoolEvent{Type: event.ConnectionCheckedIn, ConnectionID: 1})
	if got := testutil.ToFloat64(mongoPoolInUse); got != before {
		t.Errorf("mongo_pool_in_use = %v, want %v after balanced checkout+checkin", got, before)
	}
}

func TestMongoMonitors_CheckOutFailedCleansUp(t *testing.T) {
	mon := NewMongoPoolMonitor()
	mon.Event(&event.PoolEvent{Type: event.ConnectionCheckOutStarted, ConnectionID: 42})
	mon.Event(&event.PoolEvent{Type: event.ConnectionCheckOutFailed, ConnectionID: 42})
	mon.Event(&event.PoolEvent{Type: event.ConnectionCheckedOut, ConnectionID: 42})
}

func TestMongoCommandMonitor_RecordsByOp(t *testing.T) {
	mon := NewMongoCommandMonitor()
	if mon == nil || mon.Succeeded == nil || mon.Failed == nil {
		t.Fatal("CommandMonitor not wired")
	}
	mon.Succeeded(context.Background(), &event.CommandSucceededEvent{
		CommandFinishedEvent: event.CommandFinishedEvent{CommandName: "find", Duration: 5 * time.Millisecond},
	})
	mon.Failed(context.Background(), &event.CommandFailedEvent{
		CommandFinishedEvent: event.CommandFinishedEvent{CommandName: "listCollections", Duration: 3 * time.Millisecond},
	})
}
