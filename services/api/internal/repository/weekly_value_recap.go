package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/f1xgun/onevoice/pkg/domain"
)

type weeklyValueRecapRepository struct{ pool pgxPool }

func NewWeeklyValueRecapRepository(pool pgxPool) domain.WeeklyValueRecapRepository {
	return &weeklyValueRecapRepository{pool: pool}
}

func (r *weeklyValueRecapRepository) Upsert(ctx context.Context, recap domain.WeeklyValueRecap) error {
	if recap.BusinessID == uuid.Nil || recap.WeekStart.IsZero() || recap.WeekEnd.IsZero() {
		return fmt.Errorf("upsert weekly value recap: business and week are required")
	}
	const q = `INSERT INTO weekly_value_recaps
		(business_id, week_start, week_end, published_posts, dispatched_review_replies, completed_syncs)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (business_id, week_start) DO UPDATE SET
		week_end=EXCLUDED.week_end, published_posts=EXCLUDED.published_posts,
		dispatched_review_replies=EXCLUDED.dispatched_review_replies,
		completed_syncs=EXCLUDED.completed_syncs`
	if _, err := r.pool.Exec(ctx, q, recap.BusinessID, recap.WeekStart, recap.WeekEnd,
		recap.PublishedPosts, recap.DispatchedReviewReplies, recap.CompletedSyncs); err != nil {
		return fmt.Errorf("upsert weekly value recap: %w", err)
	}
	return nil
}

func (r *weeklyValueRecapRepository) GetForWeek(ctx context.Context, businessID uuid.UUID, weekStart time.Time) (*domain.WeeklyValueRecap, error) {
	const q = `SELECT id,business_id,week_start,week_end,published_posts,
		dispatched_review_replies,completed_syncs,created_at
		FROM weekly_value_recaps WHERE business_id=$1 AND week_start=$2`
	var v domain.WeeklyValueRecap
	err := r.pool.QueryRow(ctx, q, businessID, weekStart).Scan(&v.ID, &v.BusinessID, &v.WeekStart,
		&v.WeekEnd, &v.PublishedPosts, &v.DispatchedReviewReplies, &v.CompletedSyncs, &v.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get weekly value recap: %w", err)
	}
	return &v, nil
}

func (r *weeklyValueRecapRepository) EnumerateActiveBusinessIDs(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := r.pool.Query(ctx, `SELECT id FROM businesses WHERE deleted_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("enumerate recap businesses: %w", err)
	}
	ids, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (uuid.UUID, error) {
		var id uuid.UUID
		err := row.Scan(&id)
		return id, err
	})
	if err != nil {
		return nil, fmt.Errorf("collect recap businesses: %w", err)
	}
	return ids, nil
}

type weeklyValueRecapSource struct {
	posts, reviews, tasks *mongo.Collection
}

func NewWeeklyValueRecapSource(db *mongo.Database) domain.WeeklyValueRecapSource {
	return &weeklyValueRecapSource{posts: db.Collection("posts"), reviews: db.Collection("reviews"), tasks: db.Collection("agent_tasks")}
}

var recapSyncTypes = bson.A{"sync_title", "sync_description", "sync_photo", "sync_info", "sync_hours"}
var recapSyncPlatforms = bson.A{"telegram", "vk", "yandex_business", "google_business"}

// EnsureWeeklyValueRecapIndexes creates the three bounded-count indexes. It is
// invoked only when the dark-launched worker is enabled.
func EnsureWeeklyValueRecapIndexes(ctx context.Context, db *mongo.Database) error {
	models := []struct {
		collection string
		name       string
		keys       bson.D
	}{
		{"posts", "posts_recap_business_status_published", bson.D{{Key: "business_id", Value: 1}, {Key: "status", Value: 1}, {Key: "published_at", Value: 1}}},
		{"reviews", "reviews_recap_business_status_replied_dispatch", bson.D{{Key: "business_id", Value: 1}, {Key: "reply_status", Value: 1}, {Key: "replied_at", Value: 1}, {Key: "dispatch_approval_id", Value: 1}}},
		{"agent_tasks", "tasks_recap_business_status_type_platform_completed", bson.D{{Key: "business_id", Value: 1}, {Key: "status", Value: 1}, {Key: "type", Value: 1}, {Key: "platform", Value: 1}, {Key: "completed_at", Value: 1}}},
	}
	for _, model := range models {
		_, err := db.Collection(model.collection).Indexes().CreateOne(ctx, mongo.IndexModel{
			Keys: model.keys, Options: options.Index().SetName(model.name),
		})
		if err != nil {
			return fmt.Errorf("ensure weekly recap %s index: %w", model.collection, err)
		}
	}
	return nil
}

func (r *weeklyValueRecapSource) CountWeeklyValue(ctx context.Context, businessID uuid.UUID, from, to time.Time) (domain.WeeklyValueRecap, error) {
	window := bson.M{"$gte": from, "$lt": to}
	posts, err := r.posts.CountDocuments(ctx, bson.M{"business_id": businessID.String(), "status": "published", "published_at": window})
	if err != nil {
		return domain.WeeklyValueRecap{}, fmt.Errorf("count published posts: %w", err)
	}
	replies, err := r.reviews.CountDocuments(ctx, bson.M{"business_id": businessID.String(), "reply_status": domain.ReviewReplyStatusReplied, "replied_at": window, "dispatch_approval_id": bson.M{"$type": "string", "$ne": ""}})
	if err != nil {
		return domain.WeeklyValueRecap{}, fmt.Errorf("count dispatched review replies: %w", err)
	}
	profileOps := bson.A{
		bson.M{"type": bson.M{"$in": recapSyncTypes}, "platform": bson.M{"$in": recapSyncPlatforms}},
		bson.M{"type": "update_group_info", "platform": "vk", "dispatch_approval_id": bson.M{"$type": "string", "$ne": ""}},
		bson.M{"type": bson.M{"$in": bson.A{"update_info", "update_hours", "upload_photo"}}, "platform": "yandex_business", "dispatch_approval_id": bson.M{"$type": "string", "$ne": ""}},
	}
	syncs, err := r.tasks.CountDocuments(ctx, bson.M{"business_id": businessID.String(), "status": "done", "completed_at": window, "$or": profileOps})
	if err != nil {
		return domain.WeeklyValueRecap{}, fmt.Errorf("count completed syncs: %w", err)
	}
	return domain.WeeklyValueRecap{BusinessID: businessID, WeekStart: from, WeekEnd: to, PublishedPosts: int(posts), DispatchedReviewReplies: int(replies), CompletedSyncs: int(syncs)}, nil
}
