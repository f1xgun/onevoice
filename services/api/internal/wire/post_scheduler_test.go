package wire

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/f1xgun/onevoice/services/api/internal/service"
)

type schedulerTestPost struct {
	service.PostService
	ran chan struct{}
}

func (p *schedulerTestPost) PublishDue(context.Context) (int, error) {
	select {
	case p.ran <- struct{}{}:
	default:
	}
	return 0, nil
}
func TestPostSchedulerClosesAndJoins(t *testing.T) {
	posts := &schedulerTestPost{ran: make(chan struct{}, 1)}
	services := &Services{Post: posts}
	var workers sync.WaitGroup
	services.StartPostScheduler(context.Background(), &workers, slog.New(slog.NewTextHandler(io.Discard, nil)))
	select {
	case <-posts.ran:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not run its initial pass")
	}
	services.Close()
	joined := make(chan struct{})
	go func() { workers.Wait(); close(joined) }()
	select {
	case <-joined:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not join after close")
	}
}
