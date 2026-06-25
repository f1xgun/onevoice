package orchestrator_test

import (
	"sync/atomic"
	"testing"
	"time"

	natslib "github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

// TestNATSTransport_DrainSubs_StopsDeliveryButKeepsConnOpen exercises the real
// a2a.NATSTransport against an embedded server to pin the shutdown contract the
// agent runtime relies on: DrainSubs must remove interest in the agent subject
// (so no new request is delivered) while leaving the connection open and able
// to Publish — that is what lets an in-flight handler land its reply after the
// agent stopped accepting work but before the connection is torn down. If
// DrainSubs instead closed the connection (the old Close-before-Stop bug), a
// late reply Publish would fail and strand the requester on a timeout.
func TestNATSTransport_DrainSubs_StopsDeliveryButKeepsConnOpen(t *testing.T) {
	ns := startEmbeddedNATS(t)
	url := ns.ClientURL()

	subNC := connectNATS(t, url)
	transport := a2a.NewNATSTransport(subNC)

	var delivered atomic.Int32
	require.NoError(t, transport.Subscribe("tasks.testdrain", func(_, _ string, _ []byte) {
		delivered.Add(1)
	}))

	pubNC := connectNATS(t, url)
	require.NoError(t, pubNC.Publish("tasks.testdrain", []byte("before")))
	require.NoError(t, pubNC.Flush())
	require.Eventually(t, func() bool { return delivered.Load() == 1 }, time.Second, 5*time.Millisecond,
		"pre-drain message must be delivered")

	require.NoError(t, transport.DrainSubs())
	require.Eventually(t, func() bool { return subNC.NumSubscriptions() == 0 }, time.Second, 5*time.Millisecond,
		"DrainSubs must remove the subscription so no new request is delivered")

	assert.False(t, subNC.IsClosed(), "DrainSubs must NOT close the connection")
	assert.Equal(t, natslib.CONNECTED, subNC.Status(), "connection must stay connected after DrainSubs")

	require.NoError(t, pubNC.Publish("tasks.testdrain", []byte("after")))
	require.NoError(t, pubNC.Flush())
	assert.Never(t, func() bool { return delivered.Load() > 1 }, 200*time.Millisecond, 20*time.Millisecond,
		"no message must be delivered after DrainSubs")

	replyCh := make(chan []byte, 1)
	replySub, err := subNC.Subscribe("_INBOX.testdrain", func(m *natslib.Msg) { replyCh <- m.Data })
	require.NoError(t, err)
	require.NoError(t, subNC.Flush())

	require.NoError(t, transport.Publish("_INBOX.testdrain", []byte("reply")),
		"Publish on the still-open connection must succeed after DrainSubs")
	require.NoError(t, subNC.Flush())

	select {
	case got := <-replyCh:
		assert.Equal(t, []byte("reply"), got, "reply must land on the open connection")
	case <-time.After(time.Second):
		t.Fatal("reply Publish did not land after DrainSubs — connection unusable")
	}
	require.NoError(t, replySub.Unsubscribe())

	transport.Close()
	require.Eventually(t, func() bool { return subNC.IsClosed() }, 2*time.Second, 10*time.Millisecond,
		"Close must drain and close the connection")
}
