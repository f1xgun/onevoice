package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/f1xgun/onevoice/pkg/domain"
)

type businessRepository struct {
	pool pgxPool
	sb   squirrel.StatementBuilderType
}

var _ domain.BusinessRepository = (*businessRepository)(nil)

func NewBusinessRepository(pool *pgxpool.Pool) domain.BusinessRepository {
	return &businessRepository{
		pool: pool,
		sb:   squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar),
	}
}

func (r *businessRepository) Create(ctx context.Context, business *domain.Business) error {
	if business.ID == uuid.Nil {
		business.ID = uuid.New()
	}
	now := time.Now()
	business.CreatedAt = now
	business.UpdatedAt = now

	sql, args, err := r.sb.
		Insert("businesses").
		Columns("id", "name", "category", "address", "phone", "website", "description", "logo_url", "settings", "created_at", "updated_at").
		Values(business.ID, business.Name, business.Category, business.Address, business.Phone, business.Website, business.Description, business.LogoURL, business.Settings, business.CreatedAt, business.UpdatedAt).
		ToSql()
	if err != nil {
		return fmt.Errorf("build insert: %w", err)
	}

	_, err = r.pool.Exec(ctx, sql, args...)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			return domain.ErrBusinessExists
		}
		return fmt.Errorf("insert business: %w", err)
	}

	return nil
}

// CreateInTx inserts the business using a caller-supplied transaction so the
// service layer can wrap this and the business_members insert in a single tx
// (both rows commit or neither does).
func (r *businessRepository) CreateInTx(ctx context.Context, tx pgx.Tx, business *domain.Business) error {
	if business.ID == uuid.Nil {
		business.ID = uuid.New()
	}
	now := time.Now()
	business.CreatedAt = now
	business.UpdatedAt = now

	sql, args, err := r.sb.
		Insert("businesses").
		Columns("id", "name", "category", "address", "phone", "website", "description", "logo_url", "settings", "created_at", "updated_at").
		Values(business.ID, business.Name, business.Category, business.Address, business.Phone, business.Website, business.Description, business.LogoURL, business.Settings, business.CreatedAt, business.UpdatedAt).
		ToSql()
	if err != nil {
		return fmt.Errorf("build insert: %w", err)
	}

	_, err = tx.Exec(ctx, sql, args...)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			return domain.ErrBusinessExists
		}
		return fmt.Errorf("insert business: %w", err)
	}
	return nil
}

// businessColumns is the canonical select order shared by the Get paths and
// EnumerateUpcomingDeletions.
var businessColumns = []string{
	"id", "name", "category", "address", "phone", "website", "description",
	"logo_url", "settings", "deleted_at", "deletion_requested_at",
	"deletion_canceled_at", "created_at", "updated_at",
}

// scanBusiness maps one businesses row into a domain.Business in businessColumns
// order.
func scanBusiness(row scanner) (domain.Business, error) {
	var business domain.Business
	err := row.Scan(
		&business.ID,
		&business.Name,
		&business.Category,
		&business.Address,
		&business.Phone,
		&business.Website,
		&business.Description,
		&business.LogoURL,
		&business.Settings,
		&business.DeletedAt,
		&business.DeletionRequestedAt,
		&business.DeletionCanceledAt,
		&business.CreatedAt,
		&business.UpdatedAt,
	)
	return business, err
}

// GetByID returns the active organization row matching id. Soft-deleted rows
// are filtered out via `deleted_at IS NULL` and surface as ErrBusinessNotFound,
// so a soft-deleted organization disappears from the app immediately;
// deletion-aware callers use GetByIDIncludingDeleted.
func (r *businessRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Business, error) {
	sql, args, err := r.sb.
		Select(businessColumns...).
		From("businesses").
		Where(squirrel.Eq{"id": id}).
		Where("deleted_at IS NULL").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select: %w", err)
	}

	business, err := scanBusiness(r.pool.QueryRow(ctx, sql, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrBusinessNotFound
		}
		return nil, fmt.Errorf("query business: %w", err)
	}

	return &business, nil
}

// UpdateToolApprovals replaces only the settings.tool_approvals sub-object on
// the given business. Other keys inside settings (e.g. schedule) are
// preserved — this is a MERGE on the top-level settings JSONB, but a REPLACE
// on the tool_approvals sub-object: a key removed from the PUT body becomes
// un-approved (no longer in the persisted map).
//
// The write is a single server-side jsonb_set touching ONLY the tool_approvals
// key, so a concurrent writer of a sibling settings key (e.g. UpdateSchedule)
// can never revert a freshly tightened approval floor: there is no
// read-modify-write window in which a stale settings snapshot is re-persisted.
// Security-relevant — the per-tool HITL floor consumed by hitl/policy.go must
// not be silently downgraded to auto by an unrelated edit.
//
// Feeds the PUT /api/v1/business/{id}/tool-approvals
// endpoint.
func (r *businessRepository) UpdateToolApprovals(ctx context.Context, businessID uuid.UUID, approvals map[string]domain.ToolFloor) error {
	raw := make(map[string]string, len(approvals))
	for k, v := range approvals {
		raw[k] = string(v)
	}
	return r.setSettingsKeys(ctx, businessID, map[string]interface{}{
		"tool_approvals": raw,
	})
}

// UpdateSettingsKeys writes only the supplied settings sub-keys via a targeted
// jsonb_set, preserving every other key in the settings JSONB. Used by the
// schedule and voice-tone editors so each edit touches just the keys it owns
// and never rewrites the whole settings map (which would clobber a concurrent
// writer of a different sub-key, e.g. tool_approvals).
func (r *businessRepository) UpdateSettingsKeys(ctx context.Context, businessID uuid.UUID, keys map[string]interface{}) error {
	return r.setSettingsKeys(ctx, businessID, keys)
}

// setSettingsKeys applies one server-side jsonb_set per supplied sub-key
// against the settings JSONB column, leaving every other key untouched. Each
// value is marshaled to its own JSONB bind argument and the jsonb_set calls
// are chained, so two writers of DIFFERENT sub-keys cannot clobber each other:
// there is no whole-map read-modify-write window. RowsAffected==0 (missing or
// soft-deleted row) maps to ErrBusinessNotFound.
func (r *businessRepository) setSettingsKeys(ctx context.Context, businessID uuid.UUID, keys map[string]interface{}) error {
	if len(keys) == 0 {
		return nil
	}

	expr := "coalesce(settings, '{}'::jsonb)"
	args := []any{businessID}
	for key, val := range keys {
		blob, err := json.Marshal(val)
		if err != nil {
			return fmt.Errorf("marshal settings key %q: %w", key, err)
		}
		args = append(args, string(blob))
		expr = fmt.Sprintf("jsonb_set(%s, '{%s}', $%d::jsonb)", expr, quoteJSONBPathKey(key), len(args))
	}

	sql := fmt.Sprintf(
		"UPDATE businesses SET settings = %s, updated_at = NOW() WHERE id = $1 AND deleted_at IS NULL",
		expr,
	)

	tag, err := r.pool.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("update business settings: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrBusinessNotFound
	}
	return nil
}

// quoteJSONBPathKey escapes a settings sub-key for safe interpolation into a
// jsonb_set text-path literal `'{key}'`. The keys here are server-controlled
// (tool_approvals, schedule, specialDates, voiceTone), but escaping any double
// quote keeps the path literal well-formed regardless of caller input.
func quoteJSONBPathKey(key string) string {
	return strings.ReplaceAll(key, `"`, `""`)
}

// Update writes the business profile COLUMNS (name, category, address, phone,
// website, description, logo_url) only. It deliberately does NOT write the
// settings JSONB: a profile or logo edit must never re-persist a settings
// snapshot and thereby revert a concurrent settings sub-key change (e.g. a
// tightened tool-approval floor). Settings sub-keys are written through the
// targeted UpdateSettingsKeys / UpdateToolApprovals jsonb_set paths instead.
func (r *businessRepository) Update(ctx context.Context, business *domain.Business) error {
	business.UpdatedAt = time.Now()

	sql, args, err := r.sb.
		Update("businesses").
		Set("name", business.Name).
		Set("category", business.Category).
		Set("address", business.Address).
		Set("phone", business.Phone).
		Set("website", business.Website).
		Set("description", business.Description).
		Set("logo_url", business.LogoURL).
		Set("updated_at", business.UpdatedAt).
		Where(squirrel.Eq{"id": business.ID}).
		Where("deleted_at IS NULL").
		ToSql()
	if err != nil {
		return fmt.Errorf("build update: %w", err)
	}

	cmdTag, err := r.pool.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("update business: %w", err)
	}

	if cmdTag.RowsAffected() == 0 {
		return domain.ErrBusinessNotFound
	}

	return nil
}

// includingDeletedSQL builds the SELECT-by-id query (no `deleted_at IS NULL`
// filter) shared by the pool-based and tx-based deletion-aware reads so the
// column list never drifts between them.
func (r *businessRepository) includingDeletedSQL(id uuid.UUID) (sql string, args []any, err error) {
	return r.sb.
		Select(businessColumns...).
		From("businesses").
		Where(squirrel.Eq{"id": id}).
		ToSql()
}

// GetByIDIncludingDeleted is the same SELECT as GetByID minus the
// `deleted_at IS NULL` filter; lets the deletion-aware code path read the
// deletion state of a soft-deleted organization (grace banner, restore).
func (r *businessRepository) GetByIDIncludingDeleted(ctx context.Context, id uuid.UUID) (*domain.Business, error) {
	sql, args, err := r.includingDeletedSQL(id)
	if err != nil {
		return nil, fmt.Errorf("build select: %w", err)
	}
	business, err := scanBusiness(r.pool.QueryRow(ctx, sql, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrBusinessNotFound
		}
		return nil, fmt.Errorf("query business: %w", err)
	}
	return &business, nil
}

// GetByIDIncludingDeletedInTx is GetByIDIncludingDeleted reading through the
// caller-supplied tx instead of the pool, so the hard-delete sweeper's
// deletion_canceled_at re-check runs on the same connection/snapshot as the
// subsequent delete (consistency-by-construction). Defense-in-depth: the
// re-check no longer depends on the enumeration query continuing to hold its
// FOR UPDATE locks to be correct.
func (r *businessRepository) GetByIDIncludingDeletedInTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*domain.Business, error) {
	sql, args, err := r.includingDeletedSQL(id)
	if err != nil {
		return nil, fmt.Errorf("build select: %w", err)
	}
	business, err := scanBusiness(tx.QueryRow(ctx, sql, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrBusinessNotFound
		}
		return nil, fmt.Errorf("query business: %w", err)
	}
	return &business, nil
}

// RequestDeletionInTx flips active → pending, re-arming a fresh request. The
// WHERE clause matches a never-requested row OR a previously-restored one
// (deletion_canceled_at IS NOT NULL), so an organization restored within the
// grace window can schedule deletion again; the SET clears deletion_canceled_at
// so the re-armed request is genuinely pending. The follow-up classify-read
// distinguishes ErrBusinessNotFound from a still-pending (uncanceled) request →
// ErrBusinessDeletionAlreadyPending.
func (r *businessRepository) RequestDeletionInTx(ctx context.Context, tx pgx.Tx, businessID uuid.UUID) error {
	const q = `UPDATE businesses
	              SET deletion_requested_at = NOW(),
	                  deleted_at = NOW(),
	                  deletion_canceled_at = NULL,
	                  updated_at = NOW()
	            WHERE id = $1
	              AND (deletion_requested_at IS NULL OR deletion_canceled_at IS NOT NULL)`
	cmdTag, err := tx.Exec(ctx, q, businessID)
	if err != nil {
		return fmt.Errorf("request business deletion: %w", err)
	}
	if cmdTag.RowsAffected() == 0 {
		var requestedAt *time.Time
		var canceledAt *time.Time
		err2 := r.pool.QueryRow(ctx, `SELECT deletion_requested_at, deletion_canceled_at FROM businesses WHERE id = $1`, businessID).
			Scan(&requestedAt, &canceledAt)
		if err2 != nil {
			if errors.Is(err2, pgx.ErrNoRows) {
				return domain.ErrBusinessNotFound
			}
			return fmt.Errorf("classify business deletion state: %w", err2)
		}
		if requestedAt != nil && canceledAt == nil {
			return domain.ErrBusinessDeletionAlreadyPending
		}
		return domain.ErrBusinessNotFound
	}
	return nil
}

// CancelDeletion flips pending → restored via UPDATE..RETURNING gated by the
// 30-day grace boundary. Distinguishes ErrBusinessAlreadyPurged from
// ErrNoBusinessDeletionPending via a follow-up classify-read on zero matches.
func (r *businessRepository) CancelDeletion(ctx context.Context, businessID uuid.UUID, graceDays int) (bool, error) {
	sql := fmt.Sprintf(`UPDATE businesses
	                       SET deletion_canceled_at = NOW(),
	                           deleted_at = NULL,
	                           updated_at = NOW()
	                     WHERE id = $1
	                       AND deletion_requested_at IS NOT NULL
	                       AND deletion_canceled_at IS NULL
	                       AND deletion_requested_at > NOW() - INTERVAL '%d days'
	                     RETURNING id`, graceDays)
	var returnedID uuid.UUID
	err := r.pool.QueryRow(ctx, sql, businessID).Scan(&returnedID)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("cancel business deletion: %w", err)
	}
	var requestedAt *time.Time
	var canceledAt *time.Time
	err2 := r.pool.QueryRow(ctx, `SELECT deletion_requested_at, deletion_canceled_at FROM businesses WHERE id = $1`, businessID).
		Scan(&requestedAt, &canceledAt)
	if err2 != nil {
		if errors.Is(err2, pgx.ErrNoRows) {
			return false, domain.ErrBusinessAlreadyPurged
		}
		return false, fmt.Errorf("classify business cancel state: %w", err2)
	}
	if requestedAt == nil {
		return false, domain.ErrNoBusinessDeletionPending
	}
	if canceledAt != nil {
		return true, nil
	}
	return false, domain.ErrBusinessAlreadyPurged
}

// EnumeratePendingDeletionsInTx claims a batch for the hard-delete sweeper
// using FOR UPDATE SKIP LOCKED so concurrent sweepers + the cancel endpoint
// don't deadlock or race-clobber. Oldest-first for deterministic progress.
func (r *businessRepository) EnumeratePendingDeletionsInTx(ctx context.Context, tx pgx.Tx, before time.Time, limit int) ([]uuid.UUID, error) {
	const q = `SELECT id FROM businesses
	            WHERE deletion_requested_at IS NOT NULL
	              AND deletion_canceled_at IS NULL
	              AND deletion_requested_at < $1
	            ORDER BY deletion_requested_at ASC
	            FOR UPDATE SKIP LOCKED
	            LIMIT $2`
	rows, err := tx.Query(ctx, q, before, limit)
	if err != nil {
		return nil, fmt.Errorf("enumerate pending business deletions: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (uuid.UUID, error) {
		var id uuid.UUID
		err := row.Scan(&id)
		return id, err
	})
}

// HardDeleteInTx issues DELETE FROM businesses inside the caller-supplied tx.
// FK cascades remove integrations + members; the caller writes the
// business_self_deleted audit row BEFORE the DELETE in the same tx.
func (r *businessRepository) HardDeleteInTx(ctx context.Context, tx pgx.Tx, businessID uuid.UUID) error {
	cmdTag, err := tx.Exec(ctx, `DELETE FROM businesses WHERE id = $1`, businessID)
	if err != nil {
		return fmt.Errorf("hard delete business: %w", err)
	}
	if cmdTag.RowsAffected() == 0 {
		return domain.ErrBusinessNotFound
	}
	return nil
}

// BusinessDeletionExtAdapter exposes the tx-aware slice of businessRepository
// that BusinessDeletionService consumes. Concrete type so wire does not need to
// import the service package. Mirrors UserResetExtAdapter.
type BusinessDeletionExtAdapter struct {
	inner *businessRepository
}

// NewBusinessDeletionExtAdapter constructs the deletion extension repo sharing
// the pool with NewBusinessRepository via the pgxpool connection multiplex.
func NewBusinessDeletionExtAdapter(pool *pgxpool.Pool) *BusinessDeletionExtAdapter {
	return &BusinessDeletionExtAdapter{
		inner: &businessRepository{
			pool: pool,
			sb:   squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar),
		},
	}
}

// GetByIDIncludingDeleted delegates to the inner concrete repo.
func (a *BusinessDeletionExtAdapter) GetByIDIncludingDeleted(ctx context.Context, businessID uuid.UUID) (*domain.Business, error) {
	return a.inner.GetByIDIncludingDeleted(ctx, businessID)
}

// GetByIDIncludingDeletedInTx delegates to the inner concrete repo.
func (a *BusinessDeletionExtAdapter) GetByIDIncludingDeletedInTx(ctx context.Context, tx pgx.Tx, businessID uuid.UUID) (*domain.Business, error) {
	return a.inner.GetByIDIncludingDeletedInTx(ctx, tx, businessID)
}

// RequestDeletionInTx delegates to the inner concrete repo.
func (a *BusinessDeletionExtAdapter) RequestDeletionInTx(ctx context.Context, tx pgx.Tx, businessID uuid.UUID) error {
	return a.inner.RequestDeletionInTx(ctx, tx, businessID)
}

// CancelDeletion delegates to the inner concrete repo.
func (a *BusinessDeletionExtAdapter) CancelDeletion(ctx context.Context, businessID uuid.UUID, graceDays int) (bool, error) {
	return a.inner.CancelDeletion(ctx, businessID, graceDays)
}

// EnumeratePendingDeletionsInTx delegates to the inner concrete repo.
func (a *BusinessDeletionExtAdapter) EnumeratePendingDeletionsInTx(ctx context.Context, tx pgx.Tx, before time.Time, limit int) ([]uuid.UUID, error) {
	return a.inner.EnumeratePendingDeletionsInTx(ctx, tx, before, limit)
}

// HardDeleteInTx delegates to the inner concrete repo.
func (a *BusinessDeletionExtAdapter) HardDeleteInTx(ctx context.Context, tx pgx.Tx, businessID uuid.UUID) error {
	return a.inner.HardDeleteInTx(ctx, tx, businessID)
}
