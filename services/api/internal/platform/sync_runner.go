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
	jobs   chan syncJob
	ctx    context.Context
}

func NewSyncRunner(syncer *Syncer, queueSize int) *SyncRunner {
	if queueSize <= 0 {
		queueSize = 32
	}
	return &SyncRunner{syncer: syncer, jobs: make(chan syncJob, queueSize)}
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
	case r.jobs <- syncJob{business: job}:
	default:
		slog.Warn("platform sync queue full", "business_id", business.ID)
	}
}

func (r *SyncRunner) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-r.jobs:
			switch {
			case job.specialDates != nil:
				r.syncer.SyncSpecialDatesContext(ctx, &job.business, *job.specialDates)
			case job.scheduleOnly:
				r.syncer.SyncScheduleContext(ctx, &job.business)
			default:
				r.syncer.SyncBusinessContext(ctx, &job.business)
			}
		}
	}
}

// syncJob keeps explicit special-date changes separate from generic profile sync.
type syncJob struct {
	business     domain.Business
	specialDates *domain.SpecialDateHoursChange
	scheduleOnly bool
}

// SyncSchedule uses the existing queue for an explicit hours-only repair.
func (r *SyncRunner) SyncSchedule(business *domain.Business) error {
	if business == nil || r.ctx == nil || r.ctx.Err() != nil {
		return domain.ErrServiceUnavailable
	}
	select {
	case <-r.ctx.Done():
		return domain.ErrServiceUnavailable
	case r.jobs <- syncJob{business: *business, scheduleOnly: true}:
		return nil
	default:
		return domain.ErrServiceUnavailable
	}
}

func (r *SyncRunner) SyncSpecialDates(business *domain.Business, change domain.SpecialDateHoursChange) {
	if business == nil || r.ctx == nil {
		return
	}
	// Copy slices so edits after enqueue cannot change deletion ownership.
	change.Previous = append([]domain.SpecialDateHours{}, change.Previous...)
	change.Current = append([]domain.SpecialDateHours{}, change.Current...)
	select {
	case <-r.ctx.Done():
		slog.Warn("platform sync queue stopped", "business_id", business.ID)
	case r.jobs <- syncJob{business: *business, specialDates: &change}:
	default:
		slog.Warn("platform sync queue full", "business_id", business.ID)
	}
}
