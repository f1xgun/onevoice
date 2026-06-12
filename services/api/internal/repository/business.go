package repository

import (
	"context"
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
// Implementation: load current settings, mutate the tool_approvals key,
// write the merged settings back with Update. Done under a single pgx Exec
// call (no transaction) because this is standalone Postgres and a lost
// update only affects settings races which the frontend serializes via
// React Query's mutate() pattern.
//
// Feeds the PUT /api/v1/business/{id}/tool-approvals
// endpoint.
func (r *businessRepository) UpdateToolApprovals(ctx context.Context, businessID uuid.UUID, approvals map[string]domain.ToolFloor) error {
	business, err := r.GetByID(ctx, businessID)
	if err != nil {
		return err
	}
	if business.Settings == nil {
		business.Settings = make(map[string]interface{})
	}
	raw := make(map[string]interface{}, len(approvals))
	for k, v := range approvals {
		raw[k] = string(v)
	}
	business.Settings["tool_approvals"] = raw
	business.UpdatedAt = time.Now()

	sql, args, buildErr := r.sb.
		Update("businesses").
		Set("settings", business.Settings).
		Set("updated_at", business.UpdatedAt).
		Where(squirrel.Eq{"id": businessID}).
		ToSql()
	if buildErr != nil {
		return fmt.Errorf("build update: %w", buildErr)
	}

	tag, execErr := r.pool.Exec(ctx, sql, args...)
	if execErr != nil {
		return fmt.Errorf("update business settings: %w", execErr)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrBusinessNotFound
	}
	return nil
}

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
		Set("settings", business.Settings).
		Set("updated_at", business.UpdatedAt).
		Where(squirrel.Eq{"id": business.ID}).
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

// GetByIDIncludingDeleted is the same SELECT as GetByID minus the
// `deleted_at IS NULL` filter; lets the deletion-aware code path read the
// deletion state of a soft-deleted organization (grace banner, restore).
func (r *businessRepository) GetByIDIncludingDeleted(ctx context.Context, id uuid.UUID) (*domain.Business, error) {
	sql, args, err := r.sb.
		Select(businessColumns...).
		From("businesses").
		Where(squirrel.Eq{"id": id}).
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

// RequestDeletionInTx flips active → pending. The
// `deletion_requested_at IS NULL` guard makes the write idempotent; the
// follow-up classify-read distinguishes ErrBusinessNotFound from
// ErrBusinessDeletionAlreadyPending.
func (r *businessRepository) RequestDeletionInTx(ctx context.Context, tx pgx.Tx, businessID uuid.UUID) error {
	const q = `UPDATE businesses
	              SET deletion_requested_at = NOW(),
	                  deleted_at = NOW(),
	                  deletion_canceled_at = NULL,
	                  updated_at = NOW()
	            WHERE id = $1
	              AND deletion_requested_at IS NULL`
	cmdTag, err := tx.Exec(ctx, q, businessID)
	if err != nil {
		return fmt.Errorf("request business deletion: %w", err)
	}
	if cmdTag.RowsAffected() == 0 {
		var requestedAt *time.Time
		err2 := r.pool.QueryRow(ctx, `SELECT deletion_requested_at FROM businesses WHERE id = $1`, businessID).
			Scan(&requestedAt)
		if err2 != nil {
			if errors.Is(err2, pgx.ErrNoRows) {
				return domain.ErrBusinessNotFound
			}
			return fmt.Errorf("classify business deletion state: %w", err2)
		}
		if requestedAt != nil {
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
