package chatturn

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/tools"
)

// fakeAgentTaskRepo records every Create / Update call so the test can assert
// on the persisted shape. Other methods nil-panic — the tests in this file
// only exercise the onToolCall / onToolResult paths.
type fakeAgentTaskRepo struct {
	domain.AgentTaskRepository
	created []domain.AgentTask
}

func (f *fakeAgentTaskRepo) Create(_ context.Context, t *domain.AgentTask) error {
	t.ID = "fake-id"
	f.created = append(f.created, *t)
	return nil
}

// newTurnForPostalTest builds a Turn with only the AgentTasks dep wired —
// uses the unexported struct literal (accessible from same-package tests) so
// the chatturn.New panic-on-nil guards don't fire for deps the postal path
// doesn't touch.
func newTurnForPostalTest(repo domain.AgentTaskRepository) *Turn {
	return &Turn{deps: Deps{AgentTasks: repo}}
}

// TestOnToolCall_PersistsDisplayNameKey verifies the i18n catalog key
// arriving on the orchestrator SSE frame must reach the agent_tasks document
// so the FE can render the task title in the user's locale.
func TestOnToolCall_PersistsDisplayNameKey(t *testing.T) {
	repo := &fakeAgentTaskRepo{}
	turn := newTurnForPostalTest(repo)

	idMap := map[string]string{}
	turn.onToolCall(
		context.Background(),
		"biz-1",
		"call-1",
		tools.TelegramSendChannelPost,
		"Отправить пост",
		"tools.telegram.send_channel_post.name",
		map[string]interface{}{"text": "hi"},
		idMap,
	)

	require.Len(t, repo.created, 1, "onToolCall should persist exactly one task")
	got := repo.created[0]
	assert.Equal(t, "Отправить пост", got.DisplayName, "legacy DisplayName preserved")
	assert.Equal(t, "tools.telegram.send_channel_post.name", got.DisplayNameKey,
		"DisplayNameKey must reach the persisted agent_tasks document")
	assert.Equal(t, "telegram", got.Platform)
	assert.Equal(t, "send_channel_post", got.Type)
	assert.Equal(t, "running", got.Status)
}

// TestOnToolCall_EmptyDisplayNameKey_BackwardCompat — orchestrators predating
// the i18n key still persist (FE falls back to the legacy DisplayName field).
func TestOnToolCall_EmptyDisplayNameKey_BackwardCompat(t *testing.T) {
	repo := &fakeAgentTaskRepo{}
	turn := newTurnForPostalTest(repo)

	idMap := map[string]string{}
	turn.onToolCall(
		context.Background(),
		"biz-1",
		"call-1",
		tools.TelegramSendChannelPost,
		"Отправить пост",
		"",
		map[string]interface{}{"text": "hi"},
		idMap,
	)

	require.Len(t, repo.created, 1)
	assert.Equal(t, "", repo.created[0].DisplayNameKey)
	assert.Equal(t, "Отправить пост", repo.created[0].DisplayName)
}

// fakeAgentTaskRepoWithUpdate captures Update calls so the test can assert
// that ErrorCode is persisted on the AgentTask document.
type fakeAgentTaskRepoWithUpdate struct {
	fakeAgentTaskRepo
	updated []domain.AgentTask
}

func (f *fakeAgentTaskRepoWithUpdate) Update(_ context.Context, t *domain.AgentTask) error {
	f.updated = append(f.updated, *t)
	return nil
}

// TestOnToolResult_StampsErrorCode — when the SSE tool_result frame carries a
// typed Code, onToolResult must forward it onto AgentTask.ErrorCode so the
// repository writes error_code into Mongo on the same Update.
func TestOnToolResult_StampsErrorCode(t *testing.T) {
	repo := &fakeAgentTaskRepoWithUpdate{}
	turn := newTurnForPostalTest(repo)

	idMap := map[string]string{"call-1": "task-1"}
	turn.onToolResult(
		context.Background(),
		"biz-1",
		"call-1",
		map[string]interface{}{"error": "Unauthorized: bot kicked"},
		"telegram: send message: Unauthorized: bot kicked",
		"integration_token_invalid",
		idMap,
	)

	require.Len(t, repo.updated, 1)
	got := repo.updated[0]
	assert.Equal(t, "error", got.Status)
	assert.Equal(t, "Unauthorized: bot kicked", got.Error)
	assert.Equal(t, "integration_token_invalid", got.ErrorCode)
}

// TestOnToolResult_NoCode_LeavesErrorCodeEmpty — uncoded errors do not write
// an ErrorCode so the repository's selective $set leaves any prior value
// (or absence) untouched.
func TestOnToolResult_NoCode_LeavesErrorCodeEmpty(t *testing.T) {
	repo := &fakeAgentTaskRepoWithUpdate{}
	turn := newTurnForPostalTest(repo)

	idMap := map[string]string{"call-2": "task-2"}
	turn.onToolResult(
		context.Background(),
		"biz-1",
		"call-2",
		map[string]interface{}{"error": "transient network error"},
		"transient network error",
		"",
		idMap,
	)

	require.Len(t, repo.updated, 1)
	assert.Empty(t, repo.updated[0].ErrorCode)
}

// TestOnToolCall_InternalToolSkipped — internal tools (no "__" separator) do
// not surface on the Tasks page; the SSE handler must skip persistence
// regardless of the displayNameKey value.
func TestOnToolCall_InternalToolSkipped(t *testing.T) {
	repo := &fakeAgentTaskRepo{}
	turn := newTurnForPostalTest(repo)

	idMap := map[string]string{}
	turn.onToolCall(
		context.Background(),
		"biz-1",
		"call-1",
		"get_business_info",
		"Внутренний инструмент",
		"tools.internal.get_business_info.name",
		nil,
		idMap,
	)

	assert.Empty(t, repo.created, "internal tools must not be persisted as agent_tasks")
}
