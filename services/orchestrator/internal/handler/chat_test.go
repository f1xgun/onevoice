package handler_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/handler"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/orchestrator"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/tools"
)

type stubLLM struct{ content string }

func (s *stubLLM) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{Content: s.content, FinishReason: "stop"}, nil
}

// captureRunner records the RunRequest it receives, then emits a canned done
// event so the handler's SSE writer exits cleanly. Used to verify the handler
// correctly wires project fields into orchestrator.RunRequest.
type captureRunner struct {
	got orchestrator.RunRequest
}

func (c *captureRunner) Run(_ context.Context, req orchestrator.RunRequest) (<-chan orchestrator.Event, error) {
	c.got = req
	ch := make(chan orchestrator.Event, 1)
	ch <- orchestrator.Event{Type: orchestrator.EventDone}
	close(ch)
	return ch, nil
}

func TestChatHandler_SSEResponse(t *testing.T) {
	stub := &stubLLM{content: "Привет из оркестратора!"}
	reg := tools.NewRegistry()
	orch := orchestrator.New(stub, reg)

	h := handler.NewChatHandler(orch, "openai/gpt-4o-mini")

	body := `{"model":"gpt-4o-mini","message":"Привет","business_id":"biz-1","business_name":"Test Business","active_integrations":["telegram"]}`
	req := httptest.NewRequest(http.MethodPost, "/chat/conv123", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Chat(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	// Read SSE events
	scanner := bufio.NewScanner(resp.Body)
	var events []map[string]interface{}
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			var event map[string]interface{}
			data := strings.TrimPrefix(line, "data: ")
			if err := json.Unmarshal([]byte(data), &event); err == nil {
				events = append(events, event)
			}
		}
	}

	require.NotEmpty(t, events)
	// Check at least one text event with the expected content
	found := false
	for _, e := range events {
		if e["type"] == "text" && strings.Contains(e["content"].(string), "Привет из оркестратора!") {
			found = true
		}
	}
	assert.True(t, found, "expected text event with orchestrator response")
}

func TestChatHandler_MissingMessage_Returns400(t *testing.T) {
	reg := tools.NewRegistry()
	orch := orchestrator.New(&stubLLM{}, reg)
	h := handler.NewChatHandler(orch, "openai/gpt-4o-mini")

	body := `{"model":"gpt-4o-mini","business_name":"Test"}` // missing "message"
	req := httptest.NewRequest(http.MethodPost, "/chat/conv123", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Chat(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestChatHandler_with_project_context verifies that the Phase 15 project
// fields on the JSON request body flow into orchestrator.RunRequest exactly
// as the proxy would populate them in Plan 15-04.
func TestChatHandler_with_project_context(t *testing.T) {
	runner := &captureRunner{}
	h := handler.NewChatHandler(runner, "openai/gpt-4o-mini")

	body := `{
		"model": "gpt-4o-mini",
		"message": "Привет",
		"business_id": "biz-1",
		"business_name": "Test Business",
		"active_integrations": ["telegram"],
		"project_id": "proj-42",
		"project_name": "Отзывы",
		"project_system_prompt": "Отвечай вежливо",
		"project_whitelist_mode": "explicit",
		"project_allowed_tools": ["telegram__send_channel_post"]
	}`
	req := httptest.NewRequest(http.MethodPost, "/chat/conv123", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Chat(w, req)

	got := runner.got
	require.NotNil(t, got.ProjectContext, "expected ProjectContext to be constructed")
	assert.Equal(t, "proj-42", got.ProjectContext.ID)
	assert.Equal(t, "Отзывы", got.ProjectContext.Name)
	assert.Equal(t, "Отвечай вежливо", got.ProjectContext.SystemPrompt)
	assert.Equal(t, domain.WhitelistModeExplicit, got.WhitelistMode)
	require.Len(t, got.AllowedTools, 1)
	assert.Equal(t, "telegram__send_channel_post", got.AllowedTools[0])
}

// TestChatHandler_without_project_context verifies the zero-project path:
// when the request omits project_* fields, RunRequest.ProjectContext is nil
// and the whitelist mode stays empty (treated as inherit by the registry).
func TestChatHandler_without_project_context(t *testing.T) {
	runner := &captureRunner{}
	h := handler.NewChatHandler(runner, "openai/gpt-4o-mini")

	body := `{"model":"gpt-4o-mini","message":"Привет","business_id":"biz-1","business_name":"Test","active_integrations":["telegram"]}`
	req := httptest.NewRequest(http.MethodPost, "/chat/conv123", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Chat(w, req)

	got := runner.got
	assert.Nil(t, got.ProjectContext, "ProjectContext must be nil when project_id is absent")
	assert.Equal(t, domain.WhitelistMode(""), got.WhitelistMode)
	assert.Empty(t, got.AllowedTools)
}

// TestChatHandler_ThreadsPhase16Fields covers Plan 17-07 GAP-03 closure: the
// orchestrator handler must extract conversationID from the URL path and
// decode the five new Phase-16 body fields (user_id, message_id, tier,
// business_approvals, project_approval_overrides), threading them all into
// RunRequest so the pause-time persistence writes non-empty IDs to
// pending_tool_calls. Without this wiring, every persisted batch had
// conversation_id="" / business_id="" and HITL-11 hydration was impossible.
func TestChatHandler_ThreadsPhase16Fields(t *testing.T) {
	t.Run("populates RunRequest from URL + body", func(t *testing.T) {
		runner := &captureRunner{}
		h := handler.NewChatHandler(runner, "openai/gpt-4o-mini")

		userID := uuid.New().String()
		body := `{
			"model": "gpt-4o-mini",
			"message": "Привет",
			"business_id": "biz-1",
			"business_name": "Test Business",
			"active_integrations": ["telegram"],
			"user_id": "` + userID + `",
			"message_id": "msg-42",
			"tier": "pro",
			"business_approvals": {"telegram__send_channel_post":"manual"},
			"project_approval_overrides": {"vk__publish_post":"auto"}
		}`

		// Use the chi router so chi.URLParam(r, "conversationID") resolves the
		// "conv-abc-123" path segment — exactly the wiring used in
		// services/orchestrator/cmd/main.go.
		r := chi.NewRouter()
		r.Post("/chat/{conversationID}", h.Chat)
		req := httptest.NewRequest(http.MethodPost, "/chat/conv-abc-123", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		got := runner.got
		assert.Equal(t, "conv-abc-123", got.ConversationID, "conversationID must come from chi URL param")
		assert.Equal(t, "biz-1", got.BusinessID)
		assert.Equal(t, userID, got.UserIDString)
		assert.NotEqual(t, uuid.Nil, got.UserID, "valid UUID must be parsed into RunRequest.UserID")
		assert.Equal(t, "msg-42", got.MessageID)
		assert.Equal(t, "pro", got.Tier)
		require.NotNil(t, got.BusinessApprovals)
		assert.Equal(t, domain.ToolFloor("manual"), got.BusinessApprovals["telegram__send_channel_post"])
		require.NotNil(t, got.ProjectApprovalOverrides)
		assert.Equal(t, domain.ToolFloor("auto"), got.ProjectApprovalOverrides["vk__publish_post"])
	})

	t.Run("empty user_id leaves UserID zero", func(t *testing.T) {
		runner := &captureRunner{}
		h := handler.NewChatHandler(runner, "openai/gpt-4o-mini")

		body := `{
			"model": "gpt-4o-mini",
			"message": "Привет",
			"business_id": "biz-1",
			"business_name": "Test Business",
			"active_integrations": ["telegram"],
			"user_id": "",
			"message_id": "msg-1"
		}`

		r := chi.NewRouter()
		r.Post("/chat/{conversationID}", h.Chat)
		req := httptest.NewRequest(http.MethodPost, "/chat/conv-empty-user", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		got := runner.got
		assert.Equal(t, "conv-empty-user", got.ConversationID)
		assert.Equal(t, "", got.UserIDString, "empty user_id passes through verbatim")
		assert.Equal(t, uuid.Nil, got.UserID, "no UUID parsed when user_id is empty")
		assert.Equal(t, "msg-1", got.MessageID)
	})

	t.Run("invalid user_id leaves UserID zero, no panic", func(t *testing.T) {
		runner := &captureRunner{}
		h := handler.NewChatHandler(runner, "openai/gpt-4o-mini")

		body := `{
			"model": "gpt-4o-mini",
			"message": "Привет",
			"business_id": "biz-1",
			"business_name": "Test Business",
			"active_integrations": ["telegram"],
			"user_id": "not-a-uuid"
		}`

		r := chi.NewRouter()
		r.Post("/chat/{conversationID}", h.Chat)
		req := httptest.NewRequest(http.MethodPost, "/chat/conv-bad-user", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		got := runner.got
		assert.Equal(t, "not-a-uuid", got.UserIDString, "string mirror preserves the proxy-provided value")
		assert.Equal(t, uuid.Nil, got.UserID, "invalid UUID must NOT crash the handler — it logs + leaves zero")
	})
}

// TestChatHandler_invalid_whitelist_mode_falls_back verifies that a bogus
// whitelist_mode string from the proxy is coerced back to inherit ("") rather
// than crashing — handler-level defense matches the registry's default case.
func TestChatHandler_invalid_whitelist_mode_falls_back(t *testing.T) {
	runner := &captureRunner{}
	h := handler.NewChatHandler(runner, "openai/gpt-4o-mini")

	body := `{
		"model": "gpt-4o-mini",
		"message": "Привет",
		"business_id": "biz-1",
		"business_name": "Test",
		"active_integrations": ["telegram"],
		"project_id": "proj-42",
		"project_name": "P",
		"project_whitelist_mode": "bogus"
	}`
	req := httptest.NewRequest(http.MethodPost, "/chat/conv123", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Chat(w, req)

	got := runner.got
	assert.Equal(t, domain.WhitelistMode(""), got.WhitelistMode, "bogus mode must coerce to inherit")
	require.NotNil(t, got.ProjectContext)
	assert.Equal(t, "proj-42", got.ProjectContext.ID)
}
