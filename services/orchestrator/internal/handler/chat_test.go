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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/handler"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/orchestrator"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/prompt"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/tools"
)

type stubLLM struct{ content string }

func (s *stubLLM) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{Content: s.content, FinishReason: "stop"}, nil
}

func TestChatHandler_SSEResponse(t *testing.T) {
	stub := &stubLLM{content: "Привет из оркестратора!"}
	reg := tools.NewRegistry()
	orch := orchestrator.New(stub, reg)

	biz := prompt.BusinessContext{Name: "Test Business"}
	h := handler.NewChatHandler(orch, biz)

	body := `{"model":"gpt-4o-mini","message":"Привет"}`
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
	biz := prompt.BusinessContext{Name: "Test"}
	h := handler.NewChatHandler(orch, biz)

	body := `{"model":"gpt-4o-mini"}` // missing "message"
	req := httptest.NewRequest(http.MethodPost, "/chat/conv123", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Chat(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
