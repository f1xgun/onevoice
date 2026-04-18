package repository

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// --- Postgres-only unit tests (pgxmock, no env required) -----------------

// newTestProjectRepoPG builds a projectRepository whose Mongo collections are
// nil — unsafe for cascade but perfect for pure-Postgres path coverage.
func newTestProjectRepoPG(t *testing.T) (*projectRepository, pgxmock.PgxPoolIface) {
	t.Helper()
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	repo := &projectRepository{
		pool: mockPool,
		sb:   newStatementBuilder(),
		// convColl / msgColl left nil — tests that hit Mongo are handled
		// by TestProjectRepository_MongoCascade below, which uses a real
		// Mongo instance and skips cleanly when unavailable.
	}
	return repo, mockPool
}

func TestProjectRepository_Create(t *testing.T) {
	ctx := context.Background()
	businessID := uuid.New()

	t.Run("assigns id and timestamps on create", func(t *testing.T) {
		repo, mockPool := newTestProjectRepoPG(t)

		p := &domain.Project{
			BusinessID:    businessID,
			Name:          "Reviews",
			WhitelistMode: domain.WhitelistModeInherit,
			AllowedTools:  []string{},
			QuickActions:  []string{},
		}

		// Squirrel serializes uuid.UUID to string via Stringer before passing
		// to pgx; pgxmock sees a string, not the original uuid.UUID. Use
		// AnyArg() for every value to avoid driver-specific conversions.
		mockPool.ExpectExec(`INSERT INTO projects`).
			WithArgs(
				pgxmock.AnyArg(), // id
				pgxmock.AnyArg(), // business_id (serialized as string)
				"Reviews",
				"",
				"",
				"inherit",
				[]string{},
				[]string{},
				pgxmock.AnyArg(), // created_at
				pgxmock.AnyArg(), // updated_at
			).
			WillReturnResult(pgxmock.NewResult("INSERT", 1))

		err := repo.Create(ctx, p)
		require.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, p.ID)
		assert.False(t, p.CreatedAt.IsZero())
		assert.False(t, p.UpdatedAt.IsZero())
		require.NoError(t, mockPool.ExpectationsWereMet())
	})

	t.Run("duplicate key maps to ErrProjectExists", func(t *testing.T) {
		repo, mockPool := newTestProjectRepoPG(t)

		mockPool.ExpectExec(`INSERT INTO projects`).
			WithArgs(
				pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
				pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
				pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
				pgxmock.AnyArg(),
			).
			WillReturnError(errors.New("ERROR: duplicate key value violates unique constraint \"idx_projects_business_name\""))

		err := repo.Create(ctx, &domain.Project{
			BusinessID:    businessID,
			Name:          "Reviews",
			WhitelistMode: domain.WhitelistModeAll,
		})
		assert.ErrorIs(t, err, domain.ErrProjectExists)
		require.NoError(t, mockPool.ExpectationsWereMet())
	})
}

func TestProjectRepository_GetByID(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	businessID := uuid.New()
	now := time.Now()

	t.Run("returns project when row exists", func(t *testing.T) {
		repo, mockPool := newTestProjectRepoPG(t)

		rows := pgxmock.NewRows([]string{
			"id", "business_id", "name", "description", "system_prompt",
			"whitelist_mode", "allowed_tools", "quick_actions", "created_at", "updated_at",
		}).AddRow(projectID, businessID, "Reviews", "desc", "you reply to reviews",
			"explicit", []string{"telegram__send_channel_post"}, []string{"Reply nicely"},
			now, now)

		// squirrel/pgx convert uuid.UUID to string via Stringer — match with AnyArg.
		mockPool.ExpectQuery(`SELECT .+ FROM projects WHERE`).
			WithArgs(pgxmock.AnyArg()).
			WillReturnRows(rows)

		got, err := repo.GetByID(ctx, projectID)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, projectID, got.ID)
		assert.Equal(t, domain.WhitelistModeExplicit, got.WhitelistMode)
		assert.Equal(t, []string{"telegram__send_channel_post"}, got.AllowedTools)
		require.NoError(t, mockPool.ExpectationsWereMet())
	})

	t.Run("ErrNoRows maps to ErrProjectNotFound", func(t *testing.T) {
		repo, mockPool := newTestProjectRepoPG(t)

		mockPool.ExpectQuery(`SELECT .+ FROM projects WHERE`).
			WithArgs(pgxmock.AnyArg()).
			WillReturnError(pgx.ErrNoRows)

		got, err := repo.GetByID(ctx, projectID)
		assert.Nil(t, got)
		assert.ErrorIs(t, err, domain.ErrProjectNotFound)
		require.NoError(t, mockPool.ExpectationsWereMet())
	})
}

func TestProjectRepository_ListByBusinessID(t *testing.T) {
	ctx := context.Background()
	businessID := uuid.New()
	now := time.Now()

	t.Run("returns rows scoped to the business", func(t *testing.T) {
		repo, mockPool := newTestProjectRepoPG(t)

		rows := pgxmock.NewRows([]string{
			"id", "business_id", "name", "description", "system_prompt",
			"whitelist_mode", "allowed_tools", "quick_actions", "created_at", "updated_at",
		}).
			AddRow(uuid.New(), businessID, "Reviews", "", "", "all", []string{}, []string{}, now, now).
			AddRow(uuid.New(), businessID, "Posts", "", "", "inherit", []string{}, []string{}, now.Add(-time.Minute), now.Add(-time.Minute))

		mockPool.ExpectQuery(`SELECT .+ FROM projects WHERE .+ ORDER BY created_at DESC`).
			WithArgs(pgxmock.AnyArg()).
			WillReturnRows(rows)

		got, err := repo.ListByBusinessID(ctx, businessID)
		require.NoError(t, err)
		assert.Len(t, got, 2)
		assert.Equal(t, businessID, got[0].BusinessID)
		assert.Equal(t, businessID, got[1].BusinessID)
		require.NoError(t, mockPool.ExpectationsWereMet())
	})

	t.Run("returns empty slice when no rows", func(t *testing.T) {
		repo, mockPool := newTestProjectRepoPG(t)

		rows := pgxmock.NewRows([]string{
			"id", "business_id", "name", "description", "system_prompt",
			"whitelist_mode", "allowed_tools", "quick_actions", "created_at", "updated_at",
		})

		mockPool.ExpectQuery(`SELECT .+ FROM projects WHERE`).
			WithArgs(pgxmock.AnyArg()).
			WillReturnRows(rows)

		got, err := repo.ListByBusinessID(ctx, businessID)
		require.NoError(t, err)
		assert.NotNil(t, got)
		assert.Len(t, got, 0)
		require.NoError(t, mockPool.ExpectationsWereMet())
	})
}

func TestProjectRepository_Update(t *testing.T) {
	ctx := context.Background()

	t.Run("bumps updated_at and returns nil on success", func(t *testing.T) {
		repo, mockPool := newTestProjectRepoPG(t)

		p := &domain.Project{
			ID:            uuid.New(),
			BusinessID:    uuid.New(),
			Name:          "Edited",
			Description:   "desc",
			SystemPrompt:  "sp",
			WhitelistMode: domain.WhitelistModeAll,
			AllowedTools:  []string{},
			QuickActions:  []string{"quick"},
		}

		// UPDATE has 8 bound arguments (name, description, system_prompt,
		// whitelist_mode, allowed_tools, quick_actions, updated_at, id).
		mockPool.ExpectExec(`UPDATE projects SET`).
			WithArgs(
				pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
				pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			).
			WillReturnResult(pgxmock.NewResult("UPDATE", 1))

		err := repo.Update(ctx, p)
		require.NoError(t, err)
		assert.False(t, p.UpdatedAt.IsZero())
		require.NoError(t, mockPool.ExpectationsWereMet())
	})

	t.Run("zero rows affected maps to ErrProjectNotFound", func(t *testing.T) {
		repo, mockPool := newTestProjectRepoPG(t)

		mockPool.ExpectExec(`UPDATE projects SET`).
			WithArgs(
				pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
				pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			).
			WillReturnResult(pgxmock.NewResult("UPDATE", 0))

		err := repo.Update(ctx, &domain.Project{
			ID:            uuid.New(),
			BusinessID:    uuid.New(),
			Name:          "x",
			WhitelistMode: domain.WhitelistModeAll,
		})
		assert.ErrorIs(t, err, domain.ErrProjectNotFound)
		require.NoError(t, mockPool.ExpectationsWereMet())
	})
}

func TestProjectRepository_Delete(t *testing.T) {
	ctx := context.Background()

	t.Run("deletes the row and returns nil", func(t *testing.T) {
		repo, mockPool := newTestProjectRepoPG(t)
		id := uuid.New()

		mockPool.ExpectExec(`DELETE FROM projects WHERE`).
			WithArgs(pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("DELETE", 1))

		err := repo.Delete(ctx, id)
		require.NoError(t, err)
		require.NoError(t, mockPool.ExpectationsWereMet())
	})

	t.Run("zero rows affected maps to ErrProjectNotFound", func(t *testing.T) {
		repo, mockPool := newTestProjectRepoPG(t)
		id := uuid.New()

		mockPool.ExpectExec(`DELETE FROM projects WHERE`).
			WithArgs(pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("DELETE", 0))

		err := repo.Delete(ctx, id)
		assert.ErrorIs(t, err, domain.ErrProjectNotFound)
		require.NoError(t, mockPool.ExpectationsWereMet())
	})
}

// --- Mongo cascade integration test (skips when MongoDB unavailable) ------

// setupMongoTestDBForProject mirrors conversation_test.go's skip pattern.
// Skips cleanly when MongoDB is unreachable instead of failing CI.
func setupMongoTestDBForProject(t *testing.T) *mongo.Database {
	t.Helper()
	ctx := context.Background()

	mongoURI := os.Getenv("MONGODB_TEST_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(options.Client().ApplyURI(mongoURI))
	if err != nil {
		t.Skipf("MongoDB not available: %v", err)
	}

	if err := client.Ping(ctx, nil); err != nil {
		t.Skipf("MongoDB not reachable: %v", err)
	}

	t.Cleanup(func() {
		db := client.Database("test_onevoice_project")
		if err := db.Drop(ctx); err != nil {
			t.Logf("Warning: failed to drop test database: %v", err)
		}
		require.NoError(t, client.Disconnect(ctx))
	})

	return client.Database("test_onevoice_project")
}

// TestProjectRepository_CountConversationsByID verifies the Mongo count query
// that feeds the delete-confirmation dialog (D-06).
func TestProjectRepository_CountConversationsByID(t *testing.T) {
	db := setupMongoTestDBForProject(t)
	ctx := context.Background()

	projectID := uuid.New()
	otherProjectID := uuid.New()

	// Seed 3 conversations for our project + 1 for a different project.
	convColl := db.Collection("conversations")
	_, err := convColl.InsertMany(ctx, []any{
		bson.M{"_id": "c1", "project_id": projectID.String()},
		bson.M{"_id": "c2", "project_id": projectID.String()},
		bson.M{"_id": "c3", "project_id": projectID.String()},
		bson.M{"_id": "c4", "project_id": otherProjectID.String()},
	})
	require.NoError(t, err)

	repo := &projectRepository{
		pool:     nil, // not exercised
		sb:       newStatementBuilder(),
		convColl: convColl,
		msgColl:  db.Collection("messages"),
	}

	count, err := repo.CountConversationsByID(ctx, projectID)
	require.NoError(t, err)
	assert.Equal(t, 3, count)

	zero, err := repo.CountConversationsByID(ctx, uuid.New())
	require.NoError(t, err)
	assert.Equal(t, 0, zero)
}

// TestProjectRepository_HardDeleteCascade verifies the full cascade:
// Mongo messages → Mongo conversations → Postgres project row.
func TestProjectRepository_HardDeleteCascade(t *testing.T) {
	db := setupMongoTestDBForProject(t)
	ctx := context.Background()

	projectID := uuid.New()
	otherProjectID := uuid.New()

	convColl := db.Collection("conversations")
	msgColl := db.Collection("messages")

	// Seed 2 conversations for the project + 1 for a different project.
	_, err := convColl.InsertMany(ctx, []any{
		bson.M{"_id": "c1", "project_id": projectID.String()},
		bson.M{"_id": "c2", "project_id": projectID.String()},
		bson.M{"_id": "c-other", "project_id": otherProjectID.String()},
	})
	require.NoError(t, err)

	// Seed 4 messages across the project's conversations + 1 for the other project.
	_, err = msgColl.InsertMany(ctx, []any{
		bson.M{"_id": "m1", "conversation_id": "c1"},
		bson.M{"_id": "m2", "conversation_id": "c1"},
		bson.M{"_id": "m3", "conversation_id": "c2"},
		bson.M{"_id": "m4", "conversation_id": "c2"},
		bson.M{"_id": "m-other", "conversation_id": "c-other"},
	})
	require.NoError(t, err)

	// Mock pool: expect the final Postgres delete.
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	mockPool.ExpectExec(`DELETE FROM projects WHERE`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("DELETE", 1))

	repo := &projectRepository{
		pool:     mockPool,
		sb:       newStatementBuilder(),
		convColl: convColl,
		msgColl:  msgColl,
	}

	deletedConvos, deletedMessages, err := repo.HardDeleteCascade(ctx, projectID)
	require.NoError(t, err)
	assert.Equal(t, 2, deletedConvos)
	assert.Equal(t, 4, deletedMessages)
	require.NoError(t, mockPool.ExpectationsWereMet())

	// Verify other-project conversation/message untouched.
	otherConvCount, err := convColl.CountDocuments(ctx, bson.M{"project_id": otherProjectID.String()})
	require.NoError(t, err)
	assert.Equal(t, int64(1), otherConvCount)

	otherMsgCount, err := msgColl.CountDocuments(ctx, bson.M{"conversation_id": "c-other"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), otherMsgCount)
}
