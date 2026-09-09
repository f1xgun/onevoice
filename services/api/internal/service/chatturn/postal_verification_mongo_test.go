package chatturn

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/api/internal/repository"
)

func TestPostalMongoLifecyclePreservesFrozenIntentThroughSparseCompletion(t *testing.T) {
	uri := os.Getenv("MONGODB_TEST_URI")
	if uri == "" {
		t.Skip("requires explicit MONGODB_TEST_URI")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	require.NoError(t, err)
	defer func() { _ = client.Disconnect(context.Background()) }()
	db := client.Database("ov_verification_postal_" + uuid.NewString())
	defer func() { _ = db.Drop(context.Background()) }()
	repo := repository.NewAgentTaskRepository(db)
	turn := &Turn{deps: Deps{AgentTasks: repo}}
	businessID := uuid.NewString()
	ids := map[string]string{}
	args := map[string]interface{}{"group_id": "42", "title": "Exact", "phone": "+7 900"}
	turn.onToolCall(ctx, businessID, "call", tools.VKUpdateGroupInfo, "", "", args, "approval", ids)
	turn.onToolResult(ctx, businessID, "call", map[string]interface{}{"ok": true}, args, "", "", ids)
	task, err := repo.GetByID(ctx, businessID, ids["call"])
	require.NoError(t, err)
	assert.Equal(t, domain.VerificationPending, task.VerificationStatus)
	assert.Equal(t, "42", task.VerificationTarget)
	assert.Equal(t, "Exact", task.VerificationExpected["title"])
	assert.Equal(t, "+7 900", task.VerificationExpected["phone"])
	assert.EqualValues(t, 1, task.VerificationAttempt)
}
