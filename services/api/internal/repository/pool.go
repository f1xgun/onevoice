package repository

import (
	"context"

	"github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// pgxPool is a minimal interface for the database pool used by repositories.
// Both *pgxpool.Pool and pgxmock.PgxPoolIface satisfy this interface.
//
// Begin is needed by repositories that must manage their own transaction
// internally because their interface method cannot accept a caller-supplied
// pgx.Tx — billingRepository.LogUsage (the fixed llm.Writer contract) wraps the
// usage_logs INSERT and the credit-metering ledger write in one transaction.
type pgxPool interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Begin(ctx context.Context) (pgx.Tx, error)
}

// scanner is the Scan-only subset shared by pgx.Row (QueryRow path) and
// pgx.CollectableRow (CollectRows path), letting one scanX helper serve both.
type scanner interface {
	Scan(dest ...any) error
}

func newStatementBuilder() squirrel.StatementBuilderType {
	return squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar)
}
