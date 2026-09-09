package repository

import (
	"context"
	"errors"
	"fmt"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/f1xgun/onevoice/pkg/domain"
)

type contentTemplateRepository struct {
	pool pgxPool
	psql sq.StatementBuilderType
}

func NewContentTemplateRepository(pool pgxPool) domain.ContentTemplateRepository {
	return &contentTemplateRepository{pool: pool, psql: newStatementBuilder()}
}

func scanContentTemplate(row scanner) (*domain.ContentTemplate, error) {
	var template domain.ContentTemplate
	if err := row.Scan(&template.ID, &template.BusinessID, &template.CreatedBy, &template.Name,
		&template.Kind, &template.Body, &template.Placeholders, &template.CreatedAt, &template.UpdatedAt); err != nil {
		return nil, err
	}
	return &template, nil
}

const contentTemplateColumns = "id, business_id, created_by, name, kind, body, placeholders, created_at, updated_at"

func (r *contentTemplateRepository) ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]domain.ContentTemplate, error) {
	query, args, err := r.psql.Select(contentTemplateColumns).From("content_templates").
		Where(sq.Eq{"business_id": businessID}).
		Where("EXISTS (SELECT 1 FROM businesses b WHERE b.id = content_templates.business_id AND b.deleted_at IS NULL)").
		OrderBy("updated_at DESC", "id").ToSql()
	if err != nil {
		return nil, fmt.Errorf("build list content templates: %w", err)
	}
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list content templates: %w", err)
	}
	defer rows.Close()
	out := make([]domain.ContentTemplate, 0)
	for rows.Next() {
		template, scanErr := scanContentTemplate(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan content template: %w", scanErr)
		}
		out = append(out, *template)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list content templates rows: %w", err)
	}
	return out, nil
}

func (r *contentTemplateRepository) GetByID(ctx context.Context, businessID, id uuid.UUID) (*domain.ContentTemplate, error) {
	query, args, err := r.psql.Select(contentTemplateColumns).From("content_templates").
		Where(sq.Eq{"business_id": businessID, "id": id}).
		Where("EXISTS (SELECT 1 FROM businesses b WHERE b.id = content_templates.business_id AND b.deleted_at IS NULL)").ToSql()
	if err != nil {
		return nil, fmt.Errorf("build get content template: %w", err)
	}
	template, err := scanContentTemplate(r.pool.QueryRow(ctx, query, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrContentTemplateNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get content template: %w", err)
	}
	return template, nil
}

func (r *contentTemplateRepository) Create(ctx context.Context, template *domain.ContentTemplate, limit int) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin create content template: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var lockedID uuid.UUID
	if err := tx.QueryRow(ctx, "SELECT id FROM businesses WHERE id = $1 AND deleted_at IS NULL FOR UPDATE", template.BusinessID).Scan(&lockedID); errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrBusinessNotFound
	} else if err != nil {
		return fmt.Errorf("lock content template business: %w", err)
	}
	var count int
	if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM content_templates WHERE business_id = $1", template.BusinessID).Scan(&count); err != nil {
		return fmt.Errorf("count content templates: %w", err)
	}
	if count >= limit {
		return domain.ErrContentTemplateLimit
	}
	if template.ID == uuid.Nil {
		template.ID = uuid.New()
	}
	query := `INSERT INTO content_templates (id, business_id, created_by, name, kind, body, placeholders)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at, updated_at`
	if err := tx.QueryRow(ctx, query, template.ID, template.BusinessID, template.CreatedBy, template.Name,
		template.Kind, template.Body, template.Placeholders).Scan(&template.CreatedAt, &template.UpdatedAt); err != nil {
		return fmt.Errorf("insert content template: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit content template: %w", err)
	}
	return nil
}

func (r *contentTemplateRepository) Update(ctx context.Context, template *domain.ContentTemplate) error {
	query := `UPDATE content_templates AS t
		SET name = $3, kind = $4, body = $5, placeholders = $6, updated_at = NOW()
		WHERE t.id = $2 AND t.business_id = $1
		  AND EXISTS (SELECT 1 FROM businesses b WHERE b.id = t.business_id AND b.deleted_at IS NULL)
		RETURNING created_by, created_at, updated_at`
	err := r.pool.QueryRow(ctx, query, template.BusinessID, template.ID, template.Name, template.Kind,
		template.Body, template.Placeholders).Scan(&template.CreatedBy, &template.CreatedAt, &template.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrContentTemplateNotFound
	}
	if err != nil {
		return fmt.Errorf("update content template: %w", err)
	}
	return nil
}

func (r *contentTemplateRepository) Delete(ctx context.Context, businessID, id uuid.UUID) error {
	query := `DELETE FROM content_templates AS t USING businesses AS b
		WHERE t.id = $2 AND t.business_id = $1 AND b.id = t.business_id AND b.deleted_at IS NULL`
	tag, err := r.pool.Exec(ctx, query, businessID, id)
	if err != nil {
		return fmt.Errorf("delete content template: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrContentTemplateNotFound
	}
	return nil
}
