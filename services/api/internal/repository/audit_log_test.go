package repository

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// newAuditLogRepoMock returns a fresh pgxmock pool bound to the
// AuditLogRepository constructor. Mirrors the helper in invitation_test.go
// so all repository tests share the same pgxmock pattern.
func newAuditLogRepoMock(t *testing.T) (pgxmock.PgxPoolIface, domain.AuditLogRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(func() { mock.Close() })
	return mock, NewAuditLogRepository(mock)
}

// --- Insert ---

func TestAuditLogRepository_Insert_HappyPath(t *testing.T) {
	mock, repo := newAuditLogRepoMock(t)
	biz, actor := uuid.New(), uuid.New()

	// AnyArg for pointer-to-uuid values because pgx encodes (*uuid.UUID) via
	// the driver.Valuer interface and the in-test equality check is
	// brittle across pgx versions. The shape check below
	// (regex on INSERT INTO audit_logs) is the load-bearing assertion.
	mock.ExpectExec(`INSERT INTO audit_logs`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), "business.created", "business", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err := repo.Insert(context.Background(), &domain.AuditLog{
		BusinessID: &biz,
		UserID:     &actor,
		Action:     "business.created",
		Resource:   "business",
		Details:    json.RawMessage(`{"name":"acme"}`),
	})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// D-31: failed-login entries have nil BusinessID + nil UserID. Sanity check
// that the repo round-trips them straight through to pgx (which encodes
// nil pointers as NULL) without panicking on nil pointer deref.
func TestAuditLogRepository_Insert_LoginFailed_NilBusinessAndUser(t *testing.T) {
	mock, repo := newAuditLogRepoMock(t)

	mock.ExpectExec(`INSERT INTO audit_logs`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), "auth.login_failed", "user", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err := repo.Insert(context.Background(), &domain.AuditLog{
		// BusinessID + UserID intentionally nil
		Action:   "auth.login_failed",
		Resource: "user",
		Details:  json.RawMessage(`{"attempted_email":"a@b.c","ip":"1.2.3.4"}`),
	})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// Empty Details should be defaulted to "{}" so the JSONB column is never
// NULL-encoded (the column is NOT NULL on the table). Defense-in-depth
// against a hand-rolled caller that bypasses pkg/audit builders.
func TestAuditLogRepository_Insert_EmptyDetails_DefaultsToEmptyObject(t *testing.T) {
	mock, repo := newAuditLogRepoMock(t)
	biz, actor := uuid.New(), uuid.New()

	// The squirrel binder pulls Values() args literally; we cannot easily
	// assert the substituted "{}" payload via regex on the SQL because the
	// JSON content is a positional bind variable. Match the arg directly:
	// the 5th positional arg must be json.RawMessage(`{}`).
	mock.ExpectExec(`INSERT INTO audit_logs`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), "business.updated", "business", json.RawMessage(`{}`)).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err := repo.Insert(context.Background(), &domain.AuditLog{
		BusinessID: &biz,
		UserID:     &actor,
		Action:     "business.updated",
		Resource:   "business",
		// Details left empty: should be substituted to "{}" by Insert.
	})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAuditLogRepository_Insert_NilEntry_Error(t *testing.T) {
	_, repo := newAuditLogRepoMock(t)
	err := repo.Insert(context.Background(), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "audit log is required")
}

func TestAuditLogRepository_Insert_DBError(t *testing.T) {
	mock, repo := newAuditLogRepoMock(t)
	biz := uuid.New()

	mock.ExpectExec(`INSERT INTO audit_logs`).
		WillReturnError(errors.New("pg connection lost"))

	err := repo.Insert(context.Background(), &domain.AuditLog{
		BusinessID: &biz,
		Action:     "rbac.role_granted",
		Resource:   "role",
		Details:    json.RawMessage(`{}`),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "insert audit_log")
}

// --- ListByBusiness ---

func TestAuditLogRepository_ListByBusiness_AllFiltersAndCursor(t *testing.T) {
	mock, repo := newAuditLogRepoMock(t)
	biz, actor := uuid.New(), uuid.New()
	from := time.Now().Add(-7 * 24 * time.Hour).UTC()
	to := time.Now().UTC()
	cursorT := time.Now().Add(-1 * time.Hour).UTC()
	cursorID := uuid.New()

	// Order of squirrel args, mirroring the WHERE clause build order in
	// ListByBusiness:
	//   business_id, action(LIKE), action(=), user_id, created_at>=, created_at<, cursorTime, cursorID
	mock.ExpectQuery(`SELECT .+ FROM audit_logs WHERE`).
		WithArgs(
			pgxmock.AnyArg(), // business_id
			"rbac.%",         // category LIKE
			"rbac.role_granted",
			pgxmock.AnyArg(),
			from,
			to,
			cursorT,
			pgxmock.AnyArg(),
		).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "business_id", "user_id", "action", "resource", "details", "created_at",
		}).
			AddRow(uuid.New(), &biz, &actor, "rbac.role_granted", "role", json.RawMessage(`{}`), time.Now().UTC()))

	rows, err := repo.ListByBusiness(context.Background(), biz, domain.AuditLogFilter{
		Category:   "rbac",
		Action:     "rbac.role_granted",
		ActorID:    &actor,
		From:       &from,
		To:         &to,
		CursorTime: &cursorT,
		CursorID:   &cursorID,
		Limit:      50,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "rbac.role_granted", rows[0].Action)
	require.NoError(t, mock.ExpectationsWereMet())
}

// Limit=0 → defaultListLimit (50). Assertion uses a SQL regex anchored on
// the LIMIT clause; squirrel emits "LIMIT 50" inline (LIMIT is not a bound
// parameter in squirrel.Limit).
func TestAuditLogRepository_ListByBusiness_DefaultLimit(t *testing.T) {
	mock, repo := newAuditLogRepoMock(t)
	biz := uuid.New()

	mock.ExpectQuery(`FROM audit_logs WHERE business_id = \$1 ORDER BY .+ LIMIT 50`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "business_id", "user_id", "action", "resource", "details", "created_at",
		}))

	_, err := repo.ListByBusiness(context.Background(), biz, domain.AuditLogFilter{Limit: 0})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// Limit=1000 → clamped to maxListLimit (200).
func TestAuditLogRepository_ListByBusiness_ClampLimit(t *testing.T) {
	mock, repo := newAuditLogRepoMock(t)
	biz := uuid.New()

	mock.ExpectQuery(`FROM audit_logs WHERE business_id = \$1 ORDER BY .+ LIMIT 200`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "business_id", "user_id", "action", "resource", "details", "created_at",
		}))

	_, err := repo.ListByBusiness(context.Background(), biz, domain.AuditLogFilter{Limit: 1000})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// Filter-free path: only business_id pinned. Verifies no cursor predicate
// is added when CursorTime/CursorID is missing.
func TestAuditLogRepository_ListByBusiness_NoFilters(t *testing.T) {
	mock, repo := newAuditLogRepoMock(t)
	biz := uuid.New()

	mock.ExpectQuery(`FROM audit_logs WHERE business_id = \$1 ORDER BY created_at DESC, id DESC LIMIT 50`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "business_id", "user_id", "action", "resource", "details", "created_at",
		}))

	rows, err := repo.ListByBusiness(context.Background(), biz, domain.AuditLogFilter{})
	require.NoError(t, err)
	require.Empty(t, rows)
	require.NoError(t, mock.ExpectationsWereMet())
}

// Cursor predicate only added when BOTH CursorTime and CursorID are
// non-nil. Half-set cursors should be ignored — caller bug, not a SQL
// error.
func TestAuditLogRepository_ListByBusiness_HalfCursor_Ignored(t *testing.T) {
	mock, repo := newAuditLogRepoMock(t)
	biz := uuid.New()
	cursorT := time.Now().UTC()
	// CursorID intentionally nil

	mock.ExpectQuery(`FROM audit_logs WHERE business_id = \$1 ORDER BY .+ LIMIT 50`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "business_id", "user_id", "action", "resource", "details", "created_at",
		}))

	_, err := repo.ListByBusiness(context.Background(), biz, domain.AuditLogFilter{
		CursorTime: &cursorT,
		// CursorID: nil → must not add the tuple predicate
	})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAuditLogRepository_ListByBusiness_QueryError(t *testing.T) {
	mock, repo := newAuditLogRepoMock(t)
	biz := uuid.New()

	mock.ExpectQuery(`FROM audit_logs WHERE business_id`).
		WillReturnError(errors.New("connection refused"))

	_, err := repo.ListByBusiness(context.Background(), biz, domain.AuditLogFilter{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "query audit_log list")
}

// --- ListByBusinessWithActors (Plan 19-05 — LEFT JOIN users enrichment) ---

// newAuditLogRepoConcreteMock returns the underlying *auditLogRepository so
// tests can call ListByBusinessWithActors — which is NOT on
// domain.AuditLogRepository (it returns a repo-package type, AuditLogRow).
// The handler's narrow auditLogLister interface is satisfied by this same
// concrete value at wire time.
func newAuditLogRepoConcreteMock(t *testing.T) (pgxmock.PgxPoolIface, *auditLogRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(func() { mock.Close() })
	return mock, &auditLogRepository{pool: mock, sb: newStatementBuilder()}
}

// LEFT JOIN happy path: user row exists, COALESCE(u.email, '') returns the
// real email. Verifies the JOIN shape ("LEFT JOIN users u ON u.id = al.user_id"
// regex) plus actor_email column population from the scan target.
func TestAuditLogRepository_ListByBusinessWithActors_EnrichesEmail(t *testing.T) {
	mock, repo := newAuditLogRepoConcreteMock(t)
	biz, actor := uuid.New(), uuid.New()

	mock.ExpectQuery(`LEFT JOIN users u ON u.id = al.user_id`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "business_id", "user_id", "action", "resource", "details", "created_at",
			"actor_email", "actor_display_name",
		}).AddRow(
			uuid.New(), &biz, &actor, "rbac.role_granted", "role",
			json.RawMessage(`{}`), time.Now().UTC(),
			"viewer@test.local", "",
		))

	rows, err := repo.ListByBusinessWithActors(context.Background(), biz, domain.AuditLogFilter{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "viewer@test.local", rows[0].ActorEmail)
	require.Equal(t, "", rows[0].ActorDisplayName)
	require.Equal(t, "rbac.role_granted", rows[0].Action)
	require.NoError(t, mock.ExpectationsWereMet())
}

// LEFT JOIN with NULL user_id (failed-login row): COALESCE returns ''; the
// scan target stays a plain string, no nil deref. ActorID on the domain
// struct remains nil — frontend renders "Неизвестен ({email})" by reading
// details.attempted_email.
func TestAuditLogRepository_ListByBusinessWithActors_NullUserBecomesEmptyEmail(t *testing.T) {
	mock, repo := newAuditLogRepoConcreteMock(t)
	biz := uuid.New()

	mock.ExpectQuery(`LEFT JOIN users u ON u.id = al.user_id`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "business_id", "user_id", "action", "resource", "details", "created_at",
			"actor_email", "actor_display_name",
		}).AddRow(
			uuid.New(), &biz, (*uuid.UUID)(nil), "auth.login_failed", "user",
			json.RawMessage(`{"attempted_email":"a@b.c"}`), time.Now().UTC(),
			"", "",
		))

	rows, err := repo.ListByBusinessWithActors(context.Background(), biz, domain.AuditLogFilter{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "", rows[0].ActorEmail) // COALESCE → '' for unmatched JOIN
	require.Nil(t, rows[0].UserID)            // domain row keeps the NULL user_id
	require.Equal(t, "auth.login_failed", rows[0].Action)
	require.NoError(t, mock.ExpectationsWereMet())
}

// Filter / cursor threading: the new method must apply the same filter set
// as ListByBusiness. We don't re-test every combination (covered exhaustively
// for ListByBusiness above); a single all-filters case + a half-cursor
// negative pin the JOIN's filter parity in place.
func TestAuditLogRepository_ListByBusinessWithActors_AppliesAllFilters(t *testing.T) {
	mock, repo := newAuditLogRepoConcreteMock(t)
	biz, actor := uuid.New(), uuid.New()
	from := time.Now().Add(-7 * 24 * time.Hour).UTC()
	to := time.Now().UTC()
	cursorT := time.Now().Add(-1 * time.Hour).UTC()
	cursorID := uuid.New()

	// Arg order mirrors WHERE-clause build order in ListByBusinessWithActors:
	//   al.business_id, al.action LIKE, al.action =, al.user_id =,
	//   al.created_at >=, al.created_at <, cursorTime, cursorID
	mock.ExpectQuery(`FROM audit_logs al LEFT JOIN users u ON u.id = al.user_id WHERE`).
		WithArgs(
			pgxmock.AnyArg(),
			"rbac.%",
			"rbac.role_granted",
			pgxmock.AnyArg(),
			from,
			to,
			cursorT,
			pgxmock.AnyArg(),
		).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "business_id", "user_id", "action", "resource", "details", "created_at",
			"actor_email", "actor_display_name",
		}))

	_, err := repo.ListByBusinessWithActors(context.Background(), biz, domain.AuditLogFilter{
		Category:   "rbac",
		Action:     "rbac.role_granted",
		ActorID:    &actor,
		From:       &from,
		To:         &to,
		CursorTime: &cursorT,
		CursorID:   &cursorID,
		Limit:      50,
	})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- DeleteOlderThan ---

func TestAuditLogRepository_DeleteOlderThan_HappyPath(t *testing.T) {
	mock, repo := newAuditLogRepoMock(t)
	cutoff := time.Now().Add(-365 * 24 * time.Hour).UTC()

	mock.ExpectExec(`DELETE FROM audit_logs WHERE created_at < \$1`).
		WithArgs(cutoff).
		WillReturnResult(pgxmock.NewResult("DELETE", 42))

	n, err := repo.DeleteOlderThan(context.Background(), cutoff)
	require.NoError(t, err)
	require.Equal(t, int64(42), n)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAuditLogRepository_DeleteOlderThan_DBError(t *testing.T) {
	mock, repo := newAuditLogRepoMock(t)

	mock.ExpectExec(`DELETE FROM audit_logs`).
		WillReturnError(errors.New("pg down"))

	_, err := repo.DeleteOlderThan(context.Background(), time.Now())
	require.Error(t, err)
	require.Contains(t, err.Error(), "delete audit_log older than")
}

// Zero rows affected (no expired rows) is the steady-state path — must NOT
// be an error. The sweep counter still increments {result="ok"} for this
// outcome.
func TestAuditLogRepository_DeleteOlderThan_ZeroRows(t *testing.T) {
	mock, repo := newAuditLogRepoMock(t)
	cutoff := time.Now().Add(-365 * 24 * time.Hour)

	mock.ExpectExec(`DELETE FROM audit_logs`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("DELETE", 0))

	n, err := repo.DeleteOlderThan(context.Background(), cutoff)
	require.NoError(t, err)
	require.Equal(t, int64(0), n)
}
