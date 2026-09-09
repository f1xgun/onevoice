package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// WeeklyValueRecap stores aggregate completed-operation counts only.
type WeeklyValueRecap struct {
	ID                      uuid.UUID `db:"id" json:"id"`
	BusinessID              uuid.UUID `db:"business_id" json:"-"`
	WeekStart               time.Time `db:"week_start" json:"weekStart"`
	WeekEnd                 time.Time `db:"week_end" json:"weekEnd"`
	PublishedPosts          int       `db:"published_posts" json:"publishedPosts"`
	DispatchedReviewReplies int       `db:"dispatched_review_replies" json:"dispatchedReviewReplies"`
	CompletedSyncs          int       `db:"completed_syncs" json:"completedSyncs"`
	CreatedAt               time.Time `db:"created_at" json:"createdAt"`
}

func (r WeeklyValueRecap) Total() int {
	return r.PublishedPosts + r.DispatchedReviewReplies + r.CompletedSyncs
}

type WeeklyValueRecapRepository interface {
	Upsert(context.Context, WeeklyValueRecap) error
	GetForWeek(context.Context, uuid.UUID, time.Time) (*WeeklyValueRecap, error)
	EnumerateActiveBusinessIDs(context.Context) ([]uuid.UUID, error)
}

// WeeklyValueRecapSource counts only durable, authoritative Mongo records.
type WeeklyValueRecapSource interface {
	CountWeeklyValue(context.Context, uuid.UUID, time.Time, time.Time) (WeeklyValueRecap, error)
}
