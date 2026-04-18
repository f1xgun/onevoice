package taskhub_test

import (
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
		// Publish well past buffer capacity; should not block.
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
	// Should not panic / block.
	h.Publish("never-subscribed", taskhub.Event{Kind: taskhub.KindCreated})
}
