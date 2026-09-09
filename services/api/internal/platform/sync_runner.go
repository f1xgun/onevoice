package platform

import (
	"context"
	"log/slog"
	"sync"

	"github.com/f1xgun/onevoice/pkg/domain"
)

type BusinessSyncer interface {
	SyncBusiness(business *domain.Business)
}

// SyncRunner owns a bounded queue and fixed startup workers for business sync.
type SyncRunner struct {
	syncer *Syncer
	jobs   chan domain.Business
	ctx    context.Context
}

func NewSyncRunner(syncer *Syncer, queueSize int) *SyncRunner {
	if queueSize <= 0 {
		queueSize = 32
	}
	return &SyncRunner{syncer: syncer, jobs: make(chan domain.Business, queueSize)}
}

func (r *SyncRunner) Start(ctx context.Context, workers int, wg *sync.WaitGroup) {
	r.ctx = ctx
	if workers <= 0 {
		workers = 2
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); r.worker(ctx) }()
	}
}

// SyncBusiness snapshots the value before enqueueing so callers cannot mutate
// it after the request returns.
func (r *SyncRunner) SyncBusiness(business *domain.Business) {
	if business == nil || r.ctx == nil {
		return
	}
	job := *business
	select {
	case <-r.ctx.Done():
		slog.Warn("platform sync queue stopped", "business_id", business.ID)
	case r.jobs <- job:
	default:
		slog.Warn("platform sync queue full", "business_id", business.ID)
	}
}

func (r *SyncRunner) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case business := <-r.jobs:
			r.syncer.SyncBusinessContext(ctx, &business)
		}
	}
}
