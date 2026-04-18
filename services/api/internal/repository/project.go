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
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// projectRepository persists projects in Postgres and cascades deletes into
// Mongo (conversations + messages). The public constructor returns the
// domain.ProjectRepository interface so callers never depend on the concrete
// type — the wiring invariant for Plan 15-03.
type projectRepository struct {
	pool     pgxPool
	sb       squirrel.StatementBuilderType
	convColl *mongo.Collection // conversations collection (for CountConversationsByID + cascade)
	msgColl  *mongo.Collection // messages collection (for cascade)
}

// NewProjectRepository returns a domain.ProjectRepository backed by Postgres
// (for the projects table) and Mongo (for cascading hard-delete of
// conversations + messages assigned to the project). HardDeleteCascade is part
// of the interface contract (see pkg/domain/project.go) so callers never need
// a type assertion.
func NewProjectRepository(pool pgxPool, mongoDB *mongo.Database) domain.ProjectRepository {
	return &projectRepository{
		pool:     pool,
		sb:       squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar),
		convColl: mongoDB.Collection("conversations"),
		msgColl:  mongoDB.Collection("messages"),
	}
}

// Create inserts a new project row. If name collides with an existing project
// in the same business, returns domain.ErrProjectExists.
func (r *projectRepository) Create(ctx context.Context, p *domain.Project) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	now := time.Now()
	p.CreatedAt = now
	p.UpdatedAt = now

	sql, args, err := r.sb.
		Insert("projects").
		Columns("id", "business_id", "name", "description", "system_prompt",
			"whitelist_mode", "allowed_tools", "quick_actions", "created_at", "updated_at").
		Values(p.ID, p.BusinessID, p.Name, p.Description, p.SystemPrompt,
			string(p.WhitelistMode), p.AllowedTools, p.QuickActions, p.CreatedAt, p.UpdatedAt).
		ToSql()
	if err != nil {
		return fmt.Errorf("build insert: %w", err)
	}

	if _, err := r.pool.Exec(ctx, sql, args...); err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			return domain.ErrProjectExists
		}
		return fmt.Errorf("insert project: %w", err)
	}
	return nil
}

// GetByID returns a project row by its UUID. Scoping to a business is the
// caller's responsibility (service layer enforces cross-business isolation via
// the returned BusinessID field).
func (r *projectRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Project, error) {
	sql, args, err := r.sb.
		Select("id", "business_id", "name", "description", "system_prompt",
			"whitelist_mode", "allowed_tools", "quick_actions", "created_at", "updated_at").
		From("projects").
		Where(squirrel.Eq{"id": id}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select: %w", err)
	}

	var p domain.Project
	var mode string
	err = r.pool.QueryRow(ctx, sql, args...).Scan(
		&p.ID, &p.BusinessID, &p.Name, &p.Description, &p.SystemPrompt,
		&mode, &p.AllowedTools, &p.QuickActions, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrProjectNotFound
		}
		return nil, fmt.Errorf("query project: %w", err)
	}
	p.WhitelistMode = domain.WhitelistMode(mode)
	return &p, nil
}

// ListByBusinessID returns all projects for a business, sorted newest-first.
func (r *projectRepository) ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]domain.Project, error) {
	sql, args, err := r.sb.
		Select("id", "business_id", "name", "description", "system_prompt",
			"whitelist_mode", "allowed_tools", "quick_actions", "created_at", "updated_at").
		From("projects").
		Where(squirrel.Eq{"business_id": businessID}).
		OrderBy("created_at DESC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select: %w", err)
	}

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query projects: %w", err)
	}
	defer rows.Close()

	projects := make([]domain.Project, 0)
	for rows.Next() {
		var p domain.Project
		var mode string
		if err := rows.Scan(&p.ID, &p.BusinessID, &p.Name, &p.Description, &p.SystemPrompt,
			&mode, &p.AllowedTools, &p.QuickActions, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		p.WhitelistMode = domain.WhitelistMode(mode)
		projects = append(projects, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}
	return projects, nil
}

// Update modifies mutable fields (name, description, system_prompt,
// whitelist_mode, allowed_tools, quick_actions) and bumps updated_at.
func (r *projectRepository) Update(ctx context.Context, p *domain.Project) error {
	p.UpdatedAt = time.Now()

	sql, args, err := r.sb.
		Update("projects").
		Set("name", p.Name).
		Set("description", p.Description).
		Set("system_prompt", p.SystemPrompt).
		Set("whitelist_mode", string(p.WhitelistMode)).
		Set("allowed_tools", p.AllowedTools).
		Set("quick_actions", p.QuickActions).
		Set("updated_at", p.UpdatedAt).
		Where(squirrel.Eq{"id": p.ID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build update: %w", err)
	}

	tag, err := r.pool.Exec(ctx, sql, args...)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			return domain.ErrProjectExists
		}
		return fmt.Errorf("update project: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrProjectNotFound
	}
	return nil
}

// Delete removes only the Postgres row. Use HardDeleteCascade to additionally
// drop Mongo conversations and messages assigned to the project.
func (r *projectRepository) Delete(ctx context.Context, id uuid.UUID) error {
	sql, args, err := r.sb.
		Delete("projects").
		Where(squirrel.Eq{"id": id}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build delete: %w", err)
	}

	tag, err := r.pool.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrProjectNotFound
	}
	return nil
}

// CountConversationsByID returns the number of Mongo conversations currently
// assigned to the given project_id. Feeds the frontend delete-confirmation
// dialog (D-06) so users see "will also delete N chats" before confirming.
func (r *projectRepository) CountConversationsByID(ctx context.Context, id uuid.UUID) (int, error) {
	count, err := r.convColl.CountDocuments(ctx, bson.M{"project_id": id.String()})
	if err != nil {
		return 0, fmt.Errorf("count conversations: %w", err)
	}
	return int(count), nil
}

// HardDeleteCascade deletes every Mongo message whose conversation belongs to
// the project, then every Mongo conversation in the project, then the Postgres
// project row. Returns (deletedConversations, deletedMessages, err).
//
// Order matters: Mongo first, Postgres last. If the Postgres delete fails
// after Mongo succeeds, a retry re-runs cleanly (messages/conversations are
// already gone on the second attempt, so the counts reset to 0, but the
// Postgres row still vanishes). This is the "best-effort atomic" guarantee
// documented in 15-CONTEXT D-05.
func (r *projectRepository) HardDeleteCascade(ctx context.Context, id uuid.UUID) (deletedConversations, deletedMessages int, err error) {
	projectIDStr := id.String()

	// 1. Find conversation IDs so we can scope the messages delete.
	var convIDs []string
	cursor, findErr := r.convColl.Find(ctx, bson.M{"project_id": projectIDStr})
	if findErr != nil {
		return 0, 0, fmt.Errorf("find conversations for cascade: %w", findErr)
	}
	for cursor.Next(ctx) {
		var doc struct {
			ID string `bson:"_id"`
		}
		if decodeErr := cursor.Decode(&doc); decodeErr == nil {
			convIDs = append(convIDs, doc.ID)
		}
	}
	_ = cursor.Close(ctx)

	// 2. Delete messages whose conversation_id is in the cascade set.
	var msgCount int64
	if len(convIDs) > 0 {
		msgRes, msgErr := r.msgColl.DeleteMany(ctx, bson.M{"conversation_id": bson.M{"$in": convIDs}})
		if msgErr != nil {
			return 0, 0, fmt.Errorf("delete cascade messages: %w", msgErr)
		}
		msgCount = msgRes.DeletedCount
	}

	// 3. Delete conversations.
	convRes, convErr := r.convColl.DeleteMany(ctx, bson.M{"project_id": projectIDStr})
	if convErr != nil {
		return 0, int(msgCount), fmt.Errorf("delete cascade conversations: %w", convErr)
	}

	// 4. Finally, the Postgres project row.
	if delErr := r.Delete(ctx, id); delErr != nil {
		return int(convRes.DeletedCount), int(msgCount), fmt.Errorf("delete project row: %w", delErr)
	}
	return int(convRes.DeletedCount), int(msgCount), nil
}
