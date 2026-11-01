package wire

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const postSchedulePoll = 15 * time.Second

// StartPostScheduler joins the existing API worker lifecycle; no separate service.
func (s *Services) StartPostScheduler(ctx context.Context, wg *sync.WaitGroup, log *slog.Logger) {
	if s == nil || s.Post == nil {
		return
	}
	workerCtx, cancel := context.WithCancel(ctx)
	s.postSchedulerCancel = cancel
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(postSchedulePoll)
		defer ticker.Stop()
		for {
			if _, err := s.Post.PublishDue(workerCtx); err != nil && workerCtx.Err() == nil {
				log.WarnContext(workerCtx, "scheduled post pass failed", "error", err)
			}
			select {
			case <-workerCtx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
