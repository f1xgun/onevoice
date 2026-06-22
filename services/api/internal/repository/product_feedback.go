// Package repository — product_feedback.go.
//
// ProductFeedbackRepository owns SQL for the product_feedback table — the
// system of record for in-app user feedback. The Insert runs inside the
// caller's tx so the feedback row and its owner-notification outbox row
// persist atomically.
package repository

import (
	"context"
	"fmt"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ProductFeedbackRow is one feedback submission to insert. UserID/BusinessID
// are nullable; Rating is nullable (1-5); CorrelationID is nullable.
type ProductFeedbackRow struct {
	UserID        *uuid.UUID
	BusinessID    *uuid.UUID
	Category      string
	Message       string
	Page          string
	Rating        *int16
	UserAgent     string
	CorrelationID *string
}

// ProductFeedbackRepository owns SQL for product_feedback.
type ProductFeedbackRepository struct {
	pool pgxPool
	psql sq.StatementBuilderType
}

// NewProductFeedbackRepository constructs the product_feedback repo.
func NewProductFeedbackRepository(pool pgxPool) *ProductFeedbackRepository {
	return &ProductFeedbackRepository{
		pool: pool,
		psql: sq.StatementBuilder.PlaceholderFormat(sq.Dollar),
	}
}

// Insert records one feedback row inside the caller-supplied tx and returns
// the new id. The CHECK constraints on category/rating are enforced by the
// schema; the service validates inputs before reaching here.
func (r *ProductFeedbackRepository) Insert(ctx context.Context, tx pgx.Tx, row ProductFeedbackRow) (uuid.UUID, error) {
	sqlStr, args, err := r.psql.
		Insert("product_feedback").
		Columns("user_id", "business_id", "category", "message", "page", "rating", "user_agent", "correlation_id").
		Values(row.UserID, row.BusinessID, row.Category, row.Message, row.Page, row.Rating, row.UserAgent, row.CorrelationID).
		Suffix("RETURNING id").
		ToSql()
	if err != nil {
		return uuid.Nil, fmt.Errorf("product_feedback insert build: %w", err)
	}
	var id uuid.UUID
	if err := tx.QueryRow(ctx, sqlStr, args...).Scan(&id); err != nil {
		return uuid.Nil, fmt.Errorf("product_feedback insert: %w", err)
	}
	return id, nil
}
