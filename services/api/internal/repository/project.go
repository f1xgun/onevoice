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
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// projectRepository persists projects in Postgres and cascades deletes into
// Mongo (conversations + messages). The public constructor returns the
// domain.ProjectRepository interface so callers never depend on the concrete
// type — the wiring invariant.
type projectRepository struct {
	pool        pgxPool
	sb          squirrel.StatementBuilderType
	convColl    *mongo.Collection // conversations collection (for CountConversationsByID + cascade)
	msgColl     *mongo.Collection // messages collection (for cascade)
	pendingColl *mongo.Collection // pending_tool_calls collection (for cascade of HITL batches)
}

// NewProjectRepository returns a domain.ProjectRepository backed by Postgres
// (for the projects table) and Mongo (for cascading hard-delete of
// conversations + messages assigned to the project). HardDeleteCascade is part
// of the interface contract (see pkg/domain/project.go) so callers never need
// a type assertion.
func NewProjectRepository(pool pgxPool, mongoDB *mongo.Database) domain.ProjectRepository {
	return &projectRepository{
		pool:        pool,
		sb:          squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar),
		convColl:    mongoDB.Collection("conversations"),
		msgColl:     mongoDB.Collection("messages"),
		pendingColl: mongoDB.Collection("pending_tool_calls"),
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

	overridesJSON, err := marshalApprovalOverrides(p.ApprovalOverrides)
	if err != nil {
		return fmt.Errorf("marshal approval_overrides: %w", err)
	}

	sql, args, err := r.sb.
		Insert("projects").
		Columns("id", "business_id", "name", "description", "system_prompt",
			"whitelist_mode", "allowed_tools", "approval_overrides", "quick_actions", "created_at", "updated_at").
		Values(p.ID, p.BusinessID, p.Name, p.Description, p.SystemPrompt,
			string(p.WhitelistMode), p.AllowedTools, overridesJSON, p.QuickActions, p.CreatedAt, p.UpdatedAt).
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

// marshalApprovalOverrides serializes a ToolFloor map to a JSONB byte slice
// suitable for a pgx parameter. Nil maps become `{}` (the column default is
// `'{}'::jsonb`, so this keeps reads round-trip clean).
func marshalApprovalOverrides(m map[string]domain.ToolFloor) ([]byte, error) {
	if m == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(m)
}

// unmarshalApprovalOverrides is the inverse of marshalApprovalOverrides. A
// nil / missing / malformed payload becomes a nil map — callers treat this
// identically to "no overrides" because key-absence is the inherit encoding.
func unmarshalApprovalOverrides(raw []byte) map[string]domain.ToolFloor {
	if len(raw) == 0 || string(raw) == "{}" {
		return nil
	}
	var out map[string]domain.ToolFloor
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

// GetByID returns a project row by its UUID. Scoping to a business is the
// caller's responsibility (service layer enforces cross-business isolation via
// the returned BusinessID field).
//
// COALESCE(approval_overrides, '{}'::jsonb) shields callers from a
// non-migrated DB (e.g., a dev env that hasn't run the 000004/000005
// migration yet) — missing column would error at Scan, so we explicitly
// default to '{}' at the query layer.
func (r *projectRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Project, error) {
	sql, args, err := r.sb.
		Select("id", "business_id", "name", "description", "system_prompt",
			"whitelist_mode", "allowed_tools",
			"COALESCE(approval_overrides, '{}'::jsonb)::text",
			"quick_actions", "created_at", "updated_at").
		From("projects").
		Where(squirrel.Eq{"id": id}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select: %w", err)
	}

	p, err := scanProject(r.pool.QueryRow(ctx, sql, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrProjectNotFound
		}
		return nil, fmt.Errorf("query project: %w", err)
	}
	return &p, nil
}

// scanProject maps one projects row into a domain.Project, decoding
// whitelist_mode and the COALESCE'd approval_overrides JSONB text. Shared by
// the QueryRow GetByID path and the CollectRows ListByBusinessID path.
func scanProject(row scanner) (domain.Project, error) {
	var p domain.Project
	var mode string
	var overridesText string
	if err := row.Scan(
		&p.ID, &p.BusinessID, &p.Name, &p.Description, &p.SystemPrompt,
		&mode, &p.AllowedTools, &overridesText, &p.QuickActions, &p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		return domain.Project{}, err
	}
	p.WhitelistMode = domain.WhitelistMode(mode)
	p.ApprovalOverrides = unmarshalApprovalOverrides([]byte(overridesText))
	return p, nil
}

// ListByBusinessID returns all projects for a business, sorted newest-first.
func (r *projectRepository) ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]domain.Project, error) {
	sql, args, err := r.sb.
		Select("id", "business_id", "name", "description", "system_prompt",
			"whitelist_mode", "allowed_tools",
			"COALESCE(approval_overrides, '{}'::jsonb)::text",
			"quick_actions", "created_at", "updated_at").
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

	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Project, error) {
		return scanProject(row)
	})
}

// Update modifies mutable fields (name, description, system_prompt,
// whitelist_mode, allowed_tools, approval_overrides, quick_actions) and
// bumps updated_at.
//
// approval_overrides is a JSONB map. Key-absence in the
// persisted value encodes "inherit" (never a literal "inherit" string) — the
// service layer is responsible for translating inherit from the request body
// into key-absence before calling Update.
func (r *projectRepository) Update(ctx context.Context, p *domain.Project) error {
	p.UpdatedAt = time.Now()

	overridesJSON, err := marshalApprovalOverrides(p.ApprovalOverrides)
	if err != nil {
		return fmt.Errorf("marshal approval_overrides: %w", err)
	}

	sql, args, err := r.sb.
		Update("projects").
		Set("name", p.Name).
		Set("description", p.Description).
		Set("system_prompt", p.SystemPrompt).
		Set("whitelist_mode", string(p.WhitelistMode)).
		Set("allowed_tools", p.AllowedTools).
		Set("approval_overrides", overridesJSON).
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
// dialog so users see "will also delete N chats" before confirming.
func (r *projectRepository) CountConversationsByID(ctx context.Context, id uuid.UUID) (int, error) {
	count, err := r.convColl.CountDocuments(ctx, bson.M{"project_id": id.String()})
	if err != nil {
		return 0, fmt.Errorf("count conversations: %w", err)
	}
	return int(count), nil
}

// HardDeleteCascade deletes every Mongo pending_tool_calls batch and every
// Mongo message whose conversation belongs to the project, then every
// enumerated Mongo conversation, then the Postgres project row. Returns
// (deletedConversations, deletedMessages, err).
//
// Pending batches are removed first because they carry a ModelMessages snapshot
// — a full conversation-history PII copy — and are unreachable by any read path
// or the TTL sweep once their conversation is gone, so failing to cascade them
// leaves that snapshot orphaned indefinitely.
//
// Both message/conversation Mongo deletes key off the SAME enumerated convIDs
// set: messages by conversation_id $in convIDs and conversations by _id $in
// convIDs. This binds a conversation's deletion to its messages' deletion — a
// partial enumeration
// can only ever drop a matching subset of conversations+messages together,
// never conversations without their messages (which would orphan messages that
// carry no project_id and so are unreachable by any read path or sweep).
//
// A cursor error (transient getMore failure, cursor timeout) or a per-document
// decode error is therefore FATAL: we abort before any delete so a retry can
// finish the job. The enumeration is only trusted when the cursor drains
// cleanly.
//
// Order matters: Mongo first, Postgres last. If the Postgres delete fails after
// Mongo succeeds, a retry re-runs cleanly (the conversations/messages already
// gone are simply re-counted as 0, but the Postgres row still vanishes). This
// is the "best-effort atomic" guarantee.
func (r *projectRepository) HardDeleteCascade(ctx context.Context, id uuid.UUID) (deletedConversations, deletedMessages int, err error) {
	projectIDStr := id.String()

	var convIDs []string
	cursor, findErr := r.convColl.Find(ctx, bson.M{"project_id": projectIDStr})
	if findErr != nil {
		return 0, 0, fmt.Errorf("find conversations for cascade: %w", findErr)
	}
	for cursor.Next(ctx) {
		var doc struct {
			ID string `bson:"_id"`
		}
		if decodeErr := cursor.Decode(&doc); decodeErr != nil {
			_ = cursor.Close(ctx)
			return 0, 0, fmt.Errorf("decode conversation id for cascade: %w", decodeErr)
		}
		convIDs = append(convIDs, doc.ID)
	}
	if curErr := cursor.Err(); curErr != nil {
		_ = cursor.Close(ctx)
		return 0, 0, fmt.Errorf("enumerate conversations for cascade: %w", curErr)
	}
	_ = cursor.Close(ctx)

	if len(convIDs) == 0 {
		if delErr := r.Delete(ctx, id); delErr != nil {
			return 0, 0, fmt.Errorf("delete project row: %w", delErr)
		}
		return 0, 0, nil
	}

	if _, pendErr := r.pendingColl.DeleteMany(ctx, bson.M{"conversation_id": bson.M{"$in": convIDs}}); pendErr != nil {
		return 0, 0, fmt.Errorf("delete cascade pending tool calls: %w", pendErr)
	}

	msgRes, msgErr := r.msgColl.DeleteMany(ctx, bson.M{"conversation_id": bson.M{"$in": convIDs}})
	if msgErr != nil {
		return 0, 0, fmt.Errorf("delete cascade messages: %w", msgErr)
	}
	msgCount := msgRes.DeletedCount

	convRes, convErr := r.convColl.DeleteMany(ctx, bson.M{"_id": bson.M{"$in": convIDs}})
	if convErr != nil {
		return 0, int(msgCount), fmt.Errorf("delete cascade conversations: %w", convErr)
	}

	if delErr := r.Delete(ctx, id); delErr != nil {
		return int(convRes.DeletedCount), int(msgCount), fmt.Errorf("delete project row: %w", delErr)
	}
	return int(convRes.DeletedCount), int(msgCount), nil
}
