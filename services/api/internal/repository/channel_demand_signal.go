// Package repository — channel_demand_signal.go.
//
// ChannelDemandSignalRepository owns SQL for the channel_demand_signals table —
// the durable store behind the not-yet-supported-channel fake-door. Each row is
// one business's expressed interest in a channel OneVoice does not support yet
// (Avito, Wildberries, Ozon, 2GIS, other), so product can measure pull before
// building. Every read is scoped by business_id for tenant isolation.
package repository

import (
	"context"
	"fmt"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
)

// ChannelDemandSignalRow is one demand signal to insert. Note is nullable.
type ChannelDemandSignalRow struct {
	BusinessID uuid.UUID
	Channel    string
	Note       *string
}

// ChannelDemandCount is one per-channel aggregate returned by SummaryByBusiness.
type ChannelDemandCount struct {
	Channel string
	Count   int
}

// ChannelDemandSignalRepository owns SQL for channel_demand_signals.
type ChannelDemandSignalRepository struct {
	pool pgxPool
	psql sq.StatementBuilderType
}

// NewChannelDemandSignalRepository constructs the channel_demand_signals repo.
// Returns the concrete pointer (matches telemetry_events / product_feedback):
// only ChannelRequestService depends on it and the methods are used directly
// without an intervening interface.
func NewChannelDemandSignalRepository(pool pgxPool) *ChannelDemandSignalRepository {
	return &ChannelDemandSignalRepository{
		pool: pool,
		psql: sq.StatementBuilder.PlaceholderFormat(sq.Dollar),
	}
}

// Insert records one demand signal. A nil Note is stored as SQL NULL. The
// DB-level CHECK on channel and the NOT NULL business_id FK are defense in depth
// behind the app-layer enum validation.
func (r *ChannelDemandSignalRepository) Insert(ctx context.Context, row ChannelDemandSignalRow) error {
	sqlStr, args, err := r.psql.
		Insert("channel_demand_signals").
		Columns("business_id", "channel", "note").
		Values(row.BusinessID, row.Channel, row.Note).
		ToSql()
	if err != nil {
		return fmt.Errorf("channel_demand_signals insert build: %w", err)
	}
	if _, err := r.pool.Exec(ctx, sqlStr, args...); err != nil {
		return fmt.Errorf("channel_demand_signals insert: %w", err)
	}
	return nil
}

// SummaryByBusiness returns the per-channel demand count for one business,
// scoped by business_id so a caller only ever reads its own tenant's signals.
func (r *ChannelDemandSignalRepository) SummaryByBusiness(ctx context.Context, businessID uuid.UUID) ([]ChannelDemandCount, error) {
	sqlStr, args, err := r.psql.
		Select("channel", "COUNT(*)").
		From("channel_demand_signals").
		Where(sq.Eq{"business_id": businessID}).
		GroupBy("channel").
		OrderBy("channel").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("channel_demand_signals summary build: %w", err)
	}
	rows, err := r.pool.Query(ctx, sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("channel_demand_signals summary: %w", err)
	}
	defer rows.Close()

	var out []ChannelDemandCount
	for rows.Next() {
		var c ChannelDemandCount
		if err := rows.Scan(&c.Channel, &c.Count); err != nil {
			return nil, fmt.Errorf("channel_demand_signals summary scan: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("channel_demand_signals summary rows: %w", err)
	}
	return out, nil
}
