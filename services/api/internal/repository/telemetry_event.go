// Package repository — telemetry_event.go.
//
// TelemetryEventRepository owns SQL for the telemetry_events table, which
// persists product analytics events (funnel / activation / retention) on RU
// Postgres. Events are server-attributed (user_id stamped from the JWT in the
// service layer) and inserted in batches that mirror the frontend flush.
package repository

import (
	"context"
	"fmt"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
)

// TelemetryEventRow is one row to insert. UserID/BusinessID are nullable;
// Metadata is pre-marshaled JSON ('{}' when empty); CorrelationID/ClientTS
// are nullable client-supplied strings.
type TelemetryEventRow struct {
	UserID        *uuid.UUID
	BusinessID    *uuid.UUID
	EventType     string
	Action        string
	Page          string
	Metadata      []byte
	CorrelationID *string
	ClientTS      *string
}

// TelemetryEventRepository owns SQL for telemetry_events.
type TelemetryEventRepository struct {
	pool pgxPool
	psql sq.StatementBuilderType
}

// NewTelemetryEventRepository constructs the telemetry_events repo. Returns
// the concrete pointer (matches email_outbox) — only TelemetryService depends
// on it and the methods are used directly without an intervening interface.
func NewTelemetryEventRepository(pool pgxPool) *TelemetryEventRepository {
	return &TelemetryEventRepository{
		pool: pool,
		psql: sq.StatementBuilder.PlaceholderFormat(sq.Dollar),
	}
}

// InsertBatch inserts all rows in a single multi-row INSERT. A nil/empty
// Metadata is stored as the empty JSON object so the NOT NULL jsonb column
// always holds valid JSON. No-op (nil) on an empty slice.
func (r *TelemetryEventRepository) InsertBatch(ctx context.Context, rows []TelemetryEventRow) error {
	if len(rows) == 0 {
		return nil
	}
	b := r.psql.
		Insert("telemetry_events").
		Columns("user_id", "business_id", "event_type", "action", "page", "metadata", "correlation_id", "client_ts")
	for _, row := range rows {
		meta := row.Metadata
		if len(meta) == 0 {
			meta = []byte("{}")
		}
		b = b.Values(row.UserID, row.BusinessID, row.EventType, row.Action, row.Page, meta, row.CorrelationID, row.ClientTS)
	}
	sqlStr, args, err := b.ToSql()
	if err != nil {
		return fmt.Errorf("telemetry_events insert build: %w", err)
	}
	if _, err := r.pool.Exec(ctx, sqlStr, args...); err != nil {
		return fmt.Errorf("telemetry_events insert: %w", err)
	}
	return nil
}
