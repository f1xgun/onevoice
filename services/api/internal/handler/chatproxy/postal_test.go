package chatproxy

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
// only exercise the OnToolCall/OnToolResult paths.
type fakeAgentTaskRepo struct {
	domain.AgentTaskRepository
	created []domain.AgentTask
}

func (f *fakeAgentTaskRepo) Create(_ context.Context, t *domain.AgentTask) error {
	t.ID = "fake-id"
	f.created = append(f.created, *t)
	return nil
}

// TestOnToolCall_PersistsDisplayNameKey verifies the i18n catalog key
// arriving on the orchestrator SSE frame must reach the agent_tasks document
// so the FE can render the task title in the user's locale.
func TestOnToolCall_PersistsDisplayNameKey(t *testing.T) {
	repo := &fakeAgentTaskRepo{}
	svc := NewPostalService(nil, nil, repo, nil)

	idMap := map[string]string{}
	svc.OnToolCall(
		context.Background(),
		"biz-1",
		"call-1",
		tools.TelegramSendChannelPost,
		"Отправить пост",                        // legacy display name
		"tools.telegram.send_channel_post.name", // displayNameKey
		map[string]interface{}{"text": "hi"},
		idMap,
	)

	require.Len(t, repo.created, 1, "OnToolCall should persist exactly one task")
	got := repo.created[0]
	assert.Equal(t, "Отправить пост", got.DisplayName, "legacy DisplayName preserved")
	assert.Equal(t, "tools.telegram.send_channel_post.name", got.DisplayNameKey,
		"DisplayNameKey must reach the persisted agent_tasks document")
	assert.Equal(t, "telegram", got.Platform)
	assert.Equal(t, "send_channel_post", got.Type)
	assert.Equal(t, "running", got.Status)
}

// TestOnToolCall_EmptyDisplayNameKey_BackwardCompat — orchestrators predating
// Events without a key still persist (FE falls back to
// the legacy DisplayName field).
func TestOnToolCall_EmptyDisplayNameKey_BackwardCompat(t *testing.T) {
	repo := &fakeAgentTaskRepo{}
	svc := NewPostalService(nil, nil, repo, nil)

	idMap := map[string]string{}
	svc.OnToolCall(
		context.Background(),
		"biz-1",
		"call-1",
		tools.TelegramSendChannelPost,
		"Отправить пост",
		"", // no key from a legacy orchestrator
		map[string]interface{}{"text": "hi"},
		idMap,
	)

	require.Len(t, repo.created, 1)
	assert.Equal(t, "", repo.created[0].DisplayNameKey)
	assert.Equal(t, "Отправить пост", repo.created[0].DisplayName)
}

// TestOnToolCall_InternalToolSkipped — internal tools (no "__" separator) do
// not surface on the Tasks page; the SSE handler must skip persistence
// regardless of the displayNameKey value.
func TestOnToolCall_InternalToolSkipped(t *testing.T) {
	repo := &fakeAgentTaskRepo{}
	svc := NewPostalService(nil, nil, repo, nil)

	idMap := map[string]string{}
	svc.OnToolCall(
		context.Background(),
		"biz-1",
		"call-1",
		"get_business_info", // internal tool
		"Внутренний инструмент",
		"tools.internal.get_business_info.name",
		nil,
		idMap,
	)

	assert.Empty(t, repo.created, "internal tools must not be persisted as agent_tasks")
}
