package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/f1xgun/onevoice/pkg/domain"
)

type reviewRepository struct {
	collection *mongo.Collection
}

func NewReviewRepository(db *mongo.Database) domain.ReviewRepository {
	return &reviewRepository{
		collection: db.Collection("reviews"),
	}
}

func (r *reviewRepository) ListByBusinessID(ctx context.Context, businessID string, filter domain.ReviewFilter) ([]domain.Review, int, error) {
	reviews := make([]domain.Review, 0)

	f := bson.M{"business_id": businessID}
	if filter.Platform != "" {
		f["platform"] = filter.Platform
	}
	if filter.ReplyStatus != "" {
		f["reply_status"] = filter.ReplyStatus
	}

	total, err := r.collection.CountDocuments(ctx, f)
	if err != nil {
		return reviews, 0, fmt.Errorf("count reviews: %w", err)
	}

	opts := options.Find().
		SetLimit(int64(filter.Limit)).
		SetSkip(int64(filter.Offset)).
		SetSort(bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: -1}})

	cursor, err := r.collection.Find(ctx, f, opts)
	if err != nil {
		return reviews, 0, fmt.Errorf("find reviews: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	if err := cursor.All(ctx, &reviews); err != nil {
		return reviews, 0, fmt.Errorf("decode reviews: %w", err)
	}

	return reviews, int(total), nil
}

func (r *reviewRepository) GetByID(ctx context.Context, id string) (*domain.Review, error) {
	var review domain.Review
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&review)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, domain.ErrReviewNotFound
		}
		return nil, fmt.Errorf("query review: %w", err)
	}

	return &review, nil
}

func (r *reviewRepository) GetByExternalID(ctx context.Context, businessID, platform, externalID string) (*domain.Review, error) {
	var review domain.Review
	err := r.collection.FindOne(ctx, bson.M{
		"business_id": businessID,
		"platform":    platform,
		"external_id": externalID,
	}).Decode(&review)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, domain.ErrReviewNotFound
		}
		return nil, fmt.Errorf("query review by external_id: %w", err)
	}

	return &review, nil
}

// reviewUpsert builds the (filter, update) pair that upserts one review on its
// natural key (business_id, platform, external_id). Shared by Upsert and
// BulkUpsert so the persisted document shape stays identical across both paths.
func reviewUpsert(review *domain.Review) (filter, update bson.M) {
	id := review.ID
	if id == "" {
		id = uuid.NewString()
	}

	setFields := bson.M{
		"author_name": review.AuthorName,
		"rating":      review.Rating,
		"text":        review.Text,
	}
	if review.ReplyText != "" {
		setFields["reply_text"] = review.ReplyText
	}
	if len(review.PlatformMeta) > 0 {
		setFields["platform_meta"] = review.PlatformMeta
	}
	// reply_status is locally managed: an operator reply sets it to 'replied'
	// (and $unset's the draft fields). A platform sync re-emits every fetched
	// item as 'pending', so letting a sync $set reply_status would downgrade an
	// already-answered review back to 'pending' — re-surfacing it in the UI and
	// re-triggering the AI drafter every cycle. Only carry it forward when the
	// incoming value is 'replied' (the platform's own client echoed a reply, as
	// VK does); otherwise stamp it once on insert so a fresh review starts
	// 'pending'.
	if review.ReplyStatus == domain.ReviewReplyStatusReplied {
		setFields["reply_status"] = review.ReplyStatus
	}

	filter = bson.M{
		"business_id": review.BusinessID,
		"platform":    review.Platform,
		"external_id": review.ExternalID,
	}
	insertFields := bson.M{
		"_id":         id,
		"business_id": review.BusinessID,
		"platform":    review.Platform,
		"external_id": review.ExternalID,
		// created_at must never mutate after first insert, so it lives in
		// $setOnInsert rather than $set.
		"created_at": review.CreatedAt,
	}
	if _, set := setFields["reply_status"]; !set {
		insertFields["reply_status"] = review.ReplyStatus
	}
	update = bson.M{
		"$set":         setFields,
		"$setOnInsert": insertFields,
	}
	if review.ReplyStatus == domain.ReviewReplyStatusReplied {
		update["$unset"] = bson.M{
			"draft_reply":        "",
			"draft_status":       "",
			"draft_generated_at": "",
			"draft_error":        "",
		}
	}
	return filter, update
}

func (r *reviewRepository) Upsert(ctx context.Context, review *domain.Review) error {
	if review.ExternalID == "" {
		return fmt.Errorf("upsert review: external_id is required")
	}
	filter, update := reviewUpsert(review)
	if _, err := r.collection.UpdateOne(ctx, filter, update, options.UpdateOne().SetUpsert(true)); err != nil {
		return fmt.Errorf("upsert review: %w", err)
	}
	return nil
}

// reviewNaturalKey is the (business_id, platform, external_id) tuple a review
// upserts on. Two reviews sharing it address the same document.
type reviewNaturalKey struct {
	businessID string
	platform   string
	externalID string
}

// BulkUpsert upserts every review with a non-empty external_id in a single
// unordered BulkWrite — one round-trip instead of one UpdateOne per review.
// Unordered so a single failing model does not abort the rest of the batch.
//
// Reviews sharing a natural key are collapsed to a single model, keeping the
// last occurrence (the freshest copy from the caller). Without this, an
// unordered BulkWrite of two upserts on the same key both miss on filter and
// both insert — two documents for one review on any non-unique-index path, and
// an E11000 that drops the second under the unique index.
func (r *reviewRepository) BulkUpsert(ctx context.Context, reviews []*domain.Review) error {
	seen := make(map[reviewNaturalKey]int, len(reviews))
	deduped := make([]*domain.Review, 0, len(reviews))
	for _, review := range reviews {
		if review.ExternalID == "" {
			continue
		}
		key := reviewNaturalKey{review.BusinessID, review.Platform, review.ExternalID}
		if idx, ok := seen[key]; ok {
			deduped[idx] = review
			continue
		}
		seen[key] = len(deduped)
		deduped = append(deduped, review)
	}

	models := make([]mongo.WriteModel, 0, len(deduped))
	for _, review := range deduped {
		filter, update := reviewUpsert(review)
		models = append(models, mongo.NewUpdateOneModel().
			SetFilter(filter).
			SetUpdate(update).
			SetUpsert(true))
	}
	if len(models) == 0 {
		return nil
	}
	if _, err := r.collection.BulkWrite(ctx, models, options.BulkWrite().SetOrdered(false)); err != nil {
		return fmt.Errorf("bulk upsert reviews: %w", err)
	}
	return nil
}

// reviewBusinessExternalIndexName is the name of the compound index over the
// upsert natural key {business_id, platform, external_id}. Shared by
// EnsureReviewIndexes and MigrateReviewsBusinessScopedUniqueIndex so both
// converge on the same named, UNIQUE index.
const reviewBusinessExternalIndexName = "reviews_business_platform_external"

// EnsureReviewIndexes creates the reviews collection's compound indexes
// idempotently at API startup, so the hot read paths run as indexed lookups
// rather than collection scans.
//
//   - {business_id, platform, external_id} is the upsert natural key and is
//     UNIQUE: external_id is per-business (VK builds "{post_id}_{comment_id}"
//     from per-community sequential ints), so the constraint must include
//     business_id or two organizations sharing an (external_id, platform) would
//     collide. Building unique requires the collection to hold no duplicates on
//     this key; the sync path dedupes its batch and the BulkWrite collapses
//     same-key models, and the one-shot migration relocates any pre-existing
//     collisions before this runs at boot.
//   - {business_id, reply_status, created_at} serves ListPendingWithoutDraft
//     (filter business_id+reply_status, sort created_at desc). created_at is set
//     at sync time and never mutated, so indexing it is safe.
//   - {business_id, reply_status, draft_accepted_unedited, created_at} serves
//     ListRepliedExamples, which sorts accepted-first then recent so the drafter
//     prefers drafts the owner accepted as few-shot exemplars. The signal key
//     leads the sort suffix, so without this index the sort would be an
//     in-memory blocking sort of the whole replied set (risking the 32MB limit as
//     a business accumulates replies). draft_accepted_unedited is written once at
//     reply time and never mutated after, so indexing it is safe.
func EnsureReviewIndexes(ctx context.Context, db *mongo.Database) error {
	coll := db.Collection("reviews")
	models := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "business_id", Value: 1},
				{Key: "platform", Value: 1},
				{Key: "external_id", Value: 1},
			},
			Options: options.Index().SetName(reviewBusinessExternalIndexName).SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "business_id", Value: 1},
				{Key: "reply_status", Value: 1},
				{Key: "created_at", Value: -1},
			},
			Options: options.Index().SetName("reviews_business_reply_status_created_desc"),
		},
		{
			Keys: bson.D{
				{Key: "business_id", Value: 1},
				{Key: "reply_status", Value: 1},
				{Key: "draft_accepted_unedited", Value: -1},
				{Key: "created_at", Value: -1},
			},
			Options: options.Index().SetName("reviews_business_reply_status_accepted_created_desc"),
		},
	}
	if _, err := coll.Indexes().CreateMany(ctx, models); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return nil
		}
		return fmt.Errorf("ensure review indexes: %w", err)
	}
	return nil
}

func (r *reviewRepository) UpdateReply(ctx context.Context, id, replyText, replyStatus string, feedback *domain.ReviewDraftFeedback) error {
	return r.updateReply(ctx, id, replyText, replyStatus, "", feedback)
}

// UpdateReplyDispatched — see domain.ReviewRepository docstring. The chat-reply
// path carries no draft-vs-final signal (the reply came from the LLM turn, not a
// stored draft), so it records no feedback.
func (r *reviewRepository) UpdateReplyDispatched(ctx context.Context, id, replyText, replyStatus, dispatchApprovalID string) error {
	return r.updateReply(ctx, id, replyText, replyStatus, dispatchApprovalID, nil)
}

func (r *reviewRepository) updateReply(ctx context.Context, id, replyText, replyStatus, dispatchApprovalID string, feedback *domain.ReviewDraftFeedback) error {
	set := bson.M{
		"reply_text":   replyText,
		"reply_status": replyStatus,
	}
	if dispatchApprovalID != "" {
		set["dispatch_approval_id"] = dispatchApprovalID
	}
	// Stamp replied_at only on the transition to "replied" so response-time math
	// has an end point. A dispatch error leaves the row "error" with no timestamp;
	// a later successful retry stamps it. This is the single write both the manual
	// reply and the chat-reply reconciliation funnel through. The owner-edit
	// feedback rides the same transition and is $set (never $unset below), so it
	// survives the clear of the transient draft_* fields.
	if replyStatus == domain.ReviewReplyStatusReplied {
		set["replied_at"] = time.Now().UTC()
		if feedback != nil {
			set["draft_accepted_unedited"] = feedback.AcceptedUnedited
			set["draft_edit_distance"] = feedback.EditDistance
		}
	}
	update := bson.M{
		"$set": set,
		"$unset": bson.M{
			"draft_reply":        "",
			"draft_status":       "",
			"draft_generated_at": "",
			"draft_error":        "",
		},
	}

	if replyStatus == domain.ReviewReplyStatusError {
		delete(update, "$unset")
	}

	result, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, update)
	if err != nil {
		return fmt.Errorf("update review reply: %w", err)
	}

	if result.MatchedCount == 0 {
		return domain.ErrReviewNotFound
	}

	return nil
}

// StampReplyDispatchApprovalID — see domain.ReviewRepository docstring.
func (r *reviewRepository) StampReplyDispatchApprovalID(ctx context.Context, businessID, platform, externalID, dispatchApprovalID string) error {
	if dispatchApprovalID == "" || externalID == "" {
		return nil
	}
	filter := bson.M{
		"business_id": businessID,
		"platform":    platform,
		"external_id": externalID,
	}
	update := bson.M{"$set": bson.M{"dispatch_approval_id": dispatchApprovalID}}
	if _, err := r.collection.UpdateOne(ctx, filter, update); err != nil {
		return fmt.Errorf("stamp review dispatch approval id: %w", err)
	}
	return nil
}

// slaProjection restricts the SLA read to the three fields the aggregate needs.
// author_name, text, reply_text and draft_* are never fetched, so no personal
// data leaves the collection on this path — the query is aggregate-safe by
// construction, mirroring the orchestrator's reviewstats statsProjection.
var slaProjection = bson.D{
	{Key: "created_at", Value: 1},
	{Key: "reply_status", Value: 1},
	{Key: "replied_at", Value: 1},
	{Key: "_id", Value: 0},
}

// ListForSLA — see domain.ReviewRepository docstring.
func (r *reviewRepository) ListForSLA(ctx context.Context, businessID string) ([]domain.Review, error) {
	if businessID == "" {
		return nil, fmt.Errorf("list reviews for sla: business id is required")
	}

	cursor, err := r.collection.Find(ctx,
		bson.M{"business_id": businessID},
		options.Find().SetProjection(slaProjection),
	)
	if err != nil {
		return nil, fmt.Errorf("find reviews for sla: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	out := make([]domain.Review, 0)
	if err := cursor.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("decode reviews for sla: %w", err)
	}
	return out, nil
}

// ratingStatsProjection restricts the presence-health rating read to the four
// fields the rating / coverage aggregate needs. It is slaProjection plus rating:
// author_name, text, reply_text and draft_* are never fetched, so no personal
// data leaves the collection on this path — the query is aggregate-safe by
// construction, mirroring the orchestrator's reviewstats statsProjection.
var ratingStatsProjection = bson.D{
	{Key: "rating", Value: 1},
	{Key: "reply_status", Value: 1},
	{Key: "created_at", Value: 1},
	{Key: "replied_at", Value: 1},
	{Key: "_id", Value: 0},
}

// ListForRatingStats — see domain.ReviewRepository docstring.
func (r *reviewRepository) ListForRatingStats(ctx context.Context, businessID string) ([]domain.Review, error) {
	if businessID == "" {
		return nil, fmt.Errorf("list reviews for rating stats: business id is required")
	}

	cursor, err := r.collection.Find(ctx,
		bson.M{"business_id": businessID},
		options.Find().SetProjection(ratingStatsProjection),
	)
	if err != nil {
		return nil, fmt.Errorf("find reviews for rating stats: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	out := make([]domain.Review, 0)
	if err := cursor.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("decode reviews for rating stats: %w", err)
	}
	return out, nil
}

// ListPendingWithoutDraft — see domain.ReviewRepository docstring.
func (r *reviewRepository) ListPendingWithoutDraft(ctx context.Context, businessID, platform string, limit int) ([]domain.Review, error) {
	if limit <= 0 {
		return nil, nil
	}

	f := bson.M{
		"business_id":  businessID,
		"reply_status": bson.M{"$in": []string{domain.ReviewReplyStatusPending, domain.ReviewReplyStatusError}},
		"$or": []bson.M{
			{"draft_status": bson.M{"$exists": false}},
			{"draft_status": ""},
			{"draft_status": domain.ReviewDraftStatusFailed},
		},
	}
	if platform != "" {
		f["platform"] = platform
	}

	opts := options.Find().
		SetLimit(int64(limit)).
		SetSort(bson.M{"created_at": -1})

	cursor, err := r.collection.Find(ctx, f, opts)
	if err != nil {
		return nil, fmt.Errorf("find pending without draft: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	out := make([]domain.Review, 0)
	if err := cursor.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("decode pending without draft: %w", err)
	}
	return out, nil
}

// ListRepliedExamples — see domain.ReviewRepository docstring.
func (r *reviewRepository) ListRepliedExamples(ctx context.Context, businessID, platform string, limit int) ([]domain.Review, error) {
	if limit <= 0 {
		return nil, nil
	}

	f := bson.M{
		"business_id":  businessID,
		"reply_status": domain.ReviewReplyStatusReplied,
		"reply_text":   bson.M{"$exists": true, "$ne": ""},
	}
	if platform != "" {
		f["platform"] = platform
	}

	// Fetch a candidate POOL (wider than limit) sorted accepted-first then recent,
	// so the accepted bias stays index-backed. draft_accepted_unedited sorts
	// true → false → missing (legacy rows) descending. selectFewShotExamples then
	// blends recency and drops near-duplicate replies before returning `limit`
	// rows: pure accepted-first amplifies whatever style already dominates (and
	// would amplify a poisoned draft), so the final set interleaves recent replies
	// and enforces diversity.
	poolLimit := limit * fewShotPoolFactor
	if poolLimit > fewShotPoolCap {
		poolLimit = fewShotPoolCap
	}
	if poolLimit < limit {
		poolLimit = limit
	}
	opts := options.Find().
		SetLimit(int64(poolLimit)).
		SetSort(bson.D{
			{Key: "draft_accepted_unedited", Value: -1},
			{Key: "created_at", Value: -1},
		})

	cursor, err := r.collection.Find(ctx, f, opts)
	if err != nil {
		return nil, fmt.Errorf("find replied examples: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	pool := make([]domain.Review, 0, poolLimit)
	if err := cursor.All(ctx, &pool); err != nil {
		return nil, fmt.Errorf("decode replied examples: %w", err)
	}
	return selectFewShotExamples(pool, limit), nil
}

const (
	// fewShotPoolFactor / fewShotPoolCap size the candidate pool ListRepliedExamples
	// fetches before selectFewShotExamples trims it back to `limit`. A pool larger
	// than the final set gives the diversity/recency blend room to drop duplicates
	// and still fill every slot; the cap bounds the read.
	fewShotPoolFactor = 4
	fewShotPoolCap    = 40

	// replyDedupeKeyRunes is how many leading runes of a normalized reply form the
	// diversity key — enough to catch a repeated canned phrasing without treating
	// two genuinely different replies that share an opening as duplicates.
	replyDedupeKeyRunes = 80
)

// selectFewShotExamples trims an accepted-first, recency-ordered candidate pool
// to at most `limit` few-shot exemplars, applying two defenses against the
// accepted-first bias over-amplifying a single style (or a poisoned draft):
//
//   - diversity: near-duplicate replies (same normalized leading text) are
//     dropped so one canned phrasing cannot fill the block;
//   - recency blend: accepted and non-accepted (recency-ordered) rows are
//     interleaved, so the model always sees some recent replies even when a
//     large accepted backlog exists.
//
// Accepted rows still lead (the interleave starts with them), preserving the
// self-improving bias without letting it dominate.
func selectFewShotExamples(pool []domain.Review, limit int) []domain.Review {
	if limit <= 0 || len(pool) == 0 {
		return pool
	}
	seen := make(map[string]bool, len(pool))
	accepted := make([]domain.Review, 0, len(pool))
	recent := make([]domain.Review, 0, len(pool))
	for i := range pool {
		key := normalizeReplyKey(pool[i].ReplyText)
		if key != "" {
			if seen[key] {
				continue
			}
			seen[key] = true
		}
		if pool[i].DraftAcceptedUnedited != nil && *pool[i].DraftAcceptedUnedited {
			accepted = append(accepted, pool[i])
		} else {
			recent = append(recent, pool[i])
		}
	}

	out := make([]domain.Review, 0, limit)
	ai, ri := 0, 0
	for len(out) < limit && (ai < len(accepted) || ri < len(recent)) {
		if ai < len(accepted) {
			out = append(out, accepted[ai])
			ai++
			if len(out) == limit {
				break
			}
		}
		if ri < len(recent) {
			out = append(out, recent[ri])
			ri++
		}
	}
	return out
}

// normalizeReplyKey lowercases, collapses whitespace, and truncates a reply to
// its leading runes so trivially-different copies of one canned reply collapse
// to the same diversity key. An empty reply yields "" (never deduped).
func normalizeReplyKey(reply string) string {
	s := strings.ToLower(strings.TrimSpace(reply))
	s = strings.Join(strings.Fields(s), " ")
	if len([]rune(s)) > replyDedupeKeyRunes {
		s = string([]rune(s)[:replyDedupeKeyRunes])
	}
	return s
}

// ClaimDraftForGenerating — see domain.ReviewRepository docstring.
func (r *reviewRepository) ClaimDraftForGenerating(ctx context.Context, id string) (bool, error) {
	filter := bson.M{
		"_id": id,
		"draft_status": bson.M{
			"$in": []interface{}{nil, "", domain.ReviewDraftStatusFailed},
		},
	}
	update := bson.M{"$set": bson.M{
		"draft_status": domain.ReviewDraftStatusGenerating,
		"draft_error":  "",
	}}

	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return false, fmt.Errorf("claim review draft: %w", err)
	}
	return result.MatchedCount == 1, nil
}

// UpdateDraft — see domain.ReviewRepository docstring.
func (r *reviewRepository) UpdateDraft(ctx context.Context, id, draft, status, errMsg string, needsReview bool) error {
	set := bson.M{"draft_status": status}
	switch status {
	case domain.ReviewDraftStatusReady:
		set["draft_reply"] = draft
		set["draft_generated_at"] = time.Now().UTC()
		set["draft_error"] = ""
		set["needs_review"] = needsReview
	case domain.ReviewDraftStatusFailed:
		set["draft_error"] = errMsg
	}

	result, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": set})
	if err != nil {
		return fmt.Errorf("update review draft: %w", err)
	}
	if result.MatchedCount == 0 {
		return domain.ErrReviewNotFound
	}
	return nil
}
