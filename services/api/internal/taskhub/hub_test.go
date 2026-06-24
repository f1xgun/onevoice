package taskhub_test

import (
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/taskhub"
)

func TestHub_SubscribeReceivesPublishedEvents(t *testing.T) {
	h := taskhub.New()
	ch, unsub := h.Subscribe("biz-1")
	defer unsub()

	want := taskhub.Event{Kind: taskhub.KindCreated, Task: domain.AgentTask{ID: "t1", BusinessID: "biz-1"}}
	h.Publish("biz-1", want)

	select {
	case got := <-ch:
		if got.Task.ID != want.Task.ID || got.Kind != want.Kind {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestHub_IsolationByBusinessID(t *testing.T) {
	h := taskhub.New()
	chA, unsubA := h.Subscribe("biz-A")
	defer unsubA()
	chB, unsubB := h.Subscribe("biz-B")
	defer unsubB()

	h.Publish("biz-A", taskhub.Event{Kind: taskhub.KindCreated, Task: domain.AgentTask{ID: "A1"}})

	select {
	case ev := <-chA:
		if ev.Task.ID != "A1" {
			t.Fatalf("biz-A got %q", ev.Task.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("biz-A didn't receive its event")
	}

	select {
	case ev := <-chB:
		t.Fatalf("biz-B should not have received event, got %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestHub_MultipleSubscribersSameBusiness(t *testing.T) {
	h := taskhub.New()
	ch1, unsub1 := h.Subscribe("biz-1")
	defer unsub1()
	ch2, unsub2 := h.Subscribe("biz-1")
	defer unsub2()

	h.Publish("biz-1", taskhub.Event{Kind: taskhub.KindUpdated, Task: domain.AgentTask{ID: "x"}})

	var wg sync.WaitGroup
	wg.Add(2)
	for _, ch := range []<-chan taskhub.Event{ch1, ch2} {
		go func(c <-chan taskhub.Event) {
			defer wg.Done()
			select {
			case ev := <-c:
				if ev.Task.ID != "x" {
					t.Errorf("got %q", ev.Task.ID)
				}
			case <-time.After(time.Second):
				t.Error("timeout")
			}
		}(ch)
	}
	wg.Wait()
}

func TestHub_UnsubClosesChannel(t *testing.T) {
	h := taskhub.New()
	ch, unsub := h.Subscribe("biz-1")
	unsub()

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("channel should be closed")
		}
	case <-time.After(time.Second):
		t.Fatal("channel not closed within timeout")
	}
}

func TestHub_PublishDoesNotBlockOnFullBuffer(t *testing.T) {
	h := taskhub.New()
	_, unsub := h.Subscribe("biz-1")
	defer unsub()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 10_000; i++ {
			h.Publish("biz-1", taskhub.Event{Kind: taskhub.KindCreated, Task: domain.AgentTask{ID: "t"}})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on full buffer")
	}
}

func TestHub_PublishNoSubscribersIsNoop(t *testing.T) {
	h := taskhub.New()
	h.Publish("never-subscribed", taskhub.Event{Kind: taskhub.KindCreated})
}

// TestHub_ConcurrentPublishUnsubNoPanic exercises the race between Publish
// sending to a subscriber channel and unsub closing it. If Publish snapshots
// the subscriber set and sends after releasing the lock, a concurrent unsub
// can close a channel still referenced by the send, causing a "send on closed
// channel" panic. Holding the read lock across the sends prevents this.
func TestHub_ConcurrentPublishUnsubNoPanic(t *testing.T) {
	const (
		businessID  = "biz-race"
		publishers  = 8
		subscribers = 8
		cycles      = 3000
	)
	h := taskhub.New()

	prevLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer slog.SetDefault(prevLogger)

	stop := make(chan struct{})

	var pubWG sync.WaitGroup
	for p := 0; p < publishers; p++ {
		pubWG.Add(1)
		go func() {
			defer pubWG.Done()
			ev := taskhub.Event{Kind: taskhub.KindUpdated, Task: domain.AgentTask{ID: "t"}}
			for {
				select {
				case <-stop:
					return
				default:
					h.Publish(businessID, ev)
				}
			}
		}()
	}

	var subWG sync.WaitGroup
	for s := 0; s < subscribers; s++ {
		subWG.Add(1)
		go func() {
			defer subWG.Done()
			for i := 0; i < cycles; i++ {
				ch, unsub := h.Subscribe(businessID)
				select {
				case <-ch:
				default:
				}
				unsub()
			}
		}()
	}

	subWG.Wait()
	close(stop)
	pubWG.Wait()
}
