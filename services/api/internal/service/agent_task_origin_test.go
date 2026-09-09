package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
)

type originTaskRepo struct {
	domain.AgentTaskRepository
	tasks []domain.AgentTask
	user  string
}

func (r *originTaskRepo) ListByBusinessID(context.Context, string, domain.TaskFilter) ([]domain.AgentTask, int, error) {
	return append([]domain.AgentTask(nil), r.tasks...), len(r.tasks), nil
}

func (r *originTaskRepo) ResolveOriginConversationIDs(_ context.Context, _, user string, _ []domain.AgentTask) (map[string]string, error) {
	r.user = user
	return map[string]string{"task": "owned-conversation"}, nil
}

func TestAgentTaskListProjectsOnlyCurrentUsersResolvedOrigin(t *testing.T) {
	businessID, userID := uuid.New(), uuid.New()
	repo := &originTaskRepo{tasks: []domain.AgentTask{{ID: "task", OriginConversationID: "stored-private"}}}
	svc := NewAgentTaskService(repo, nil, nil)
	ctx := authz.WithBusinessContext(context.Background(), authz.BusinessContext{BusinessID: businessID, UserID: userID})
	tasks, _, err := svc.List(ctx, businessID, domain.TaskFilter{})
	require.NoError(t, err)
	require.Equal(t, userID.String(), repo.user)
	require.Equal(t, "stored-private", tasks[0].OriginConversationID)
	require.Equal(t, "owned-conversation", tasks[0].ResolvedConversationID)
}

func TestAgentTaskListWithoutRequestACLDoesNotResolveOrigin(t *testing.T) {
	repo := &originTaskRepo{tasks: []domain.AgentTask{{ID: "task", OriginConversationID: "stored-private"}}}
	svc := NewAgentTaskService(repo, nil, nil)
	tasks, _, err := svc.List(context.Background(), uuid.New(), domain.TaskFilter{})
	require.NoError(t, err)
	require.Empty(t, repo.user)
	require.Empty(t, tasks[0].ResolvedConversationID)
}
