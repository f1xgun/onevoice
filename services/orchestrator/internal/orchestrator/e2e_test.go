package orchestrator_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	natsserver "github.com/nats-io/nats-server/v2/server"
	natslib "github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/natsexec"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/orchestrator"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/prompt"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/tools"
)

// startEmbeddedNATS starts an in-process NATS server on a random port.
func startEmbeddedNATS(t *testing.T) *natsserver.Server {
	t.Helper()
	opts := &natsserver.Options{
		Host:       "127.0.0.1",
		Port:       -1, // random free port
		NoLog:      true,
		NoSigs:     true,
		MaxPending: 64 << 20,
	}
	srv, err := natsserver.NewServer(opts)
	require.NoError(t, err)
	srv.Start()
	t.Cleanup(srv.Shutdown)
	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("embedded NATS server did not become ready")
	}
	return srv
}

// connectNATS creates a NATS connection to the embedded server and registers cleanup.
func connectNATS(t *testing.T, url string) *natslib.Conn {
	t.Helper()
	nc, err := natslib.Connect(url)
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	return nc
}

// TestE2E_OrchestratorNATSAgentRoundTrip verifies the full cycle:
// LLM returns tool_call → orchestrator sends via NATS → agent responds → orchestrator emits events.
func TestE2E_OrchestratorNATSAgentRoundTrip(t *testing.T) {
	ns := startEmbeddedNATS(t)
	natsURL := ns.ClientURL()

	// --- Mock VK agent ---
	agentNC := connectNATS(t, natsURL)
	mockHandler := a2a.HandlerFunc(func(_ context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
		return &a2a.ToolResponse{
			TaskID:  req.TaskID,
			Success: true,
			Result: map[string]interface{}{
				"post_id": "12345",
				"status":  "published",
			},
		}, nil
	})
	agent := a2a.NewAgent(a2a.AgentVK, a2a.NewNATSTransport(agentNC), mockHandler)
	require.NoError(t, agent.Start(context.Background()))

	// --- Orchestrator side ---
	orchNC := connectNATS(t, natsURL)
	conn := natsexec.NewNATSConn(orchNC)

	reg := tools.NewRegistry()
	reg.Register(llm.ToolDefinition{
		Type: "function",
		Function: llm.FunctionDefinition{
			Name:        "vk__publish_post",
			Description: "Публикует пост в VK",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"text":     map[string]interface{}{"type": "string"},
					"group_id": map[string]interface{}{"type": "string"},
				},
				"required": []string{"text"},
			},
		},
	}, natsexec.New(a2a.AgentVK, "vk__publish_post", conn))

	// Stub LLM: first call → tool_call, second call → text answer
	toolArgs, _ := json.Marshal(map[string]interface{}{
		"text":     "Привет, мир!",
		"group_id": "123456",
	})
	stub := &stubLLM{responses: []*llm.ChatResponse{
		{
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{{
				ID:   "call_vk_1",
				Type: "function",
				Function: llm.FunctionCall{
					Name:      "vk__publish_post",
					Arguments: string(toolArgs),
				},
			}},
		},
		{Content: "Пост успешно опубликован в VK!", FinishReason: "stop"},
	}}

	orch := orchestrator.New(stub, reg)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx = a2a.WithBusinessID(ctx, "test-biz-id")

	events, err := orch.Run(ctx, orchestrator.RunRequest{
		UserID:             uuid.New(),
		BusinessContext:    prompt.BusinessContext{Name: "Тестовый бизнес"},
		Messages:           []llm.Message{{Role: "user", Content: "Опубликуй пост в VK"}},
		ActiveIntegrations: []string{"vk"},
	})
	require.NoError(t, err)

	// Collect events
	var toolCalls, toolResults, texts []orchestrator.Event
	var gotDone bool
	for e := range events {
		switch e.Type {
		case orchestrator.EventToolCall:
			toolCalls = append(toolCalls, e)
		case orchestrator.EventToolResult:
			toolResults = append(toolResults, e)
		case orchestrator.EventText:
			texts = append(texts, e)
		case orchestrator.EventDone:
			gotDone = true
		case orchestrator.EventError:
			// ignore in this test
		}
	}

	// tool_call emitted
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "vk__publish_post", toolCalls[0].ToolName)
	assert.Equal(t, "Привет, мир!", toolCalls[0].ToolArgs["text"])

	// tool_result from agent came back
	require.Len(t, toolResults, 1)
	assert.Equal(t, "vk__publish_post", toolResults[0].ToolName)
	assert.Empty(t, toolResults[0].ToolError)
	resultMap, ok := toolResults[0].ToolResult.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "12345", resultMap["post_id"])
	assert.Equal(t, "published", resultMap["status"])

	// text response after tool execution
	require.NotEmpty(t, texts)
	assert.Contains(t, texts[0].Content, "Пост успешно опубликован")

	assert.True(t, gotDone, "should receive done event")
}

// TestE2E_AgentError verifies that agent errors propagate back through the orchestrator.
func TestE2E_AgentError(t *testing.T) {
	ns := startEmbeddedNATS(t)
	natsURL := ns.ClientURL()

	// Agent that returns an error
	agentNC := connectNATS(t, natsURL)
	errHandler := a2a.HandlerFunc(func(_ context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
		return &a2a.ToolResponse{
			TaskID:  req.TaskID,
			Success: false,
			Error:   "VK API error: access denied (code 15)",
		}, nil
	})
	agent := a2a.NewAgent(a2a.AgentVK, a2a.NewNATSTransport(agentNC), errHandler)
	require.NoError(t, agent.Start(context.Background()))

	orchNC := connectNATS(t, natsURL)
	conn := natsexec.NewNATSConn(orchNC)

	reg := tools.NewRegistry()
	reg.Register(llm.ToolDefinition{
		Type: "function",
		Function: llm.FunctionDefinition{
			Name:        "vk__publish_post",
			Description: "Публикует пост",
			Parameters:  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
	}, natsexec.New(a2a.AgentVK, "vk__publish_post", conn))

	toolArgs, _ := json.Marshal(map[string]interface{}{"text": "test"})
	stub := &stubLLM{responses: []*llm.ChatResponse{
		{
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{{
				ID:       "call_err",
				Type:     "function",
				Function: llm.FunctionCall{Name: "vk__publish_post", Arguments: string(toolArgs)},
			}},
		},
		{Content: "Не удалось опубликовать пост — ошибка доступа.", FinishReason: "stop"},
	}}

	orch := orchestrator.New(stub, reg)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	events, err := orch.Run(ctx, orchestrator.RunRequest{
		UserID:             uuid.New(),
		BusinessContext:    prompt.BusinessContext{Name: "Test"},
		Messages:           []llm.Message{{Role: "user", Content: "post"}},
		ActiveIntegrations: []string{"vk"},
	})
	require.NoError(t, err)

	var toolResults []orchestrator.Event
	for e := range events {
		if e.Type == orchestrator.EventToolResult {
			toolResults = append(toolResults, e)
		}
	}

	require.Len(t, toolResults, 1)
	assert.Equal(t, "vk__publish_post", toolResults[0].ToolName)
	assert.Contains(t, toolResults[0].ToolError, "access denied")
}

// TestE2E_MultipleAgents verifies that tools for different agents route to the correct agent.
func TestE2E_MultipleAgents(t *testing.T) {
	ns := startEmbeddedNATS(t)
	natsURL := ns.ClientURL()

	// --- Telegram agent ---
	tgNC := connectNATS(t, natsURL)
	tgAgent := a2a.NewAgent(a2a.AgentTelegram, a2a.NewNATSTransport(tgNC),
		a2a.HandlerFunc(func(_ context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
			return &a2a.ToolResponse{
				TaskID:  req.TaskID,
				Success: true,
				Result:  map[string]interface{}{"message_id": "tg_999", "platform": "telegram"},
			}, nil
		}),
	)
	require.NoError(t, tgAgent.Start(context.Background()))

	// --- VK agent ---
	vkNC := connectNATS(t, natsURL)
	vkAgent := a2a.NewAgent(a2a.AgentVK, a2a.NewNATSTransport(vkNC),
		a2a.HandlerFunc(func(_ context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
			return &a2a.ToolResponse{
				TaskID:  req.TaskID,
				Success: true,
				Result:  map[string]interface{}{"post_id": "vk_777", "platform": "vk"},
			}, nil
		}),
	)
	require.NoError(t, vkAgent.Start(context.Background()))

	// --- Orchestrator ---
	orchNC := connectNATS(t, natsURL)
	conn := natsexec.NewNATSConn(orchNC)

	reg := tools.NewRegistry()
	reg.Register(llm.ToolDefinition{
		Type:     "function",
		Function: llm.FunctionDefinition{Name: "telegram__send_channel_post", Description: "Send TG post", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
	}, natsexec.New(a2a.AgentTelegram, "telegram__send_channel_post", conn))
	reg.Register(llm.ToolDefinition{
		Type:     "function",
		Function: llm.FunctionDefinition{Name: "vk__publish_post", Description: "Send VK post", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
	}, natsexec.New(a2a.AgentVK, "vk__publish_post", conn))

	// LLM calls telegram tool first, then vk, then answers
	tgArgs, _ := json.Marshal(map[string]interface{}{"text": "tg post"})
	vkArgs, _ := json.Marshal(map[string]interface{}{"text": "vk post"})
	stub := &stubLLM{responses: []*llm.ChatResponse{
		{
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{{
				ID:       "call_tg",
				Type:     "function",
				Function: llm.FunctionCall{Name: "telegram__send_channel_post", Arguments: string(tgArgs)},
			}},
		},
		{
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{{
				ID:       "call_vk",
				Type:     "function",
				Function: llm.FunctionCall{Name: "vk__publish_post", Arguments: string(vkArgs)},
			}},
		},
		{Content: "Посты опубликованы в обоих каналах!", FinishReason: "stop"},
	}}

	orch := orchestrator.New(stub, reg)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	events, err := orch.Run(ctx, orchestrator.RunRequest{
		UserID:             uuid.New(),
		BusinessContext:    prompt.BusinessContext{Name: "Multi"},
		Messages:           []llm.Message{{Role: "user", Content: "Опубликуй и в TG и в VK"}},
		ActiveIntegrations: []string{"telegram", "vk"},
	})
	require.NoError(t, err)

	var toolResults []orchestrator.Event
	for e := range events {
		if e.Type == orchestrator.EventToolResult {
			toolResults = append(toolResults, e)
		}
	}

	require.Len(t, toolResults, 2)

	// Verify each result came from the correct agent
	platforms := map[string]string{}
	for _, tr := range toolResults {
		rm, ok := tr.ToolResult.(map[string]interface{})
		require.True(t, ok)
		platforms[tr.ToolName] = rm["platform"].(string)
	}
	assert.Equal(t, "telegram", platforms["telegram__send_channel_post"])
	assert.Equal(t, "vk", platforms["vk__publish_post"])
}

// TestE2E_BusinessIDPropagation verifies that the business ID from context
// reaches the agent through NATS.
func TestE2E_BusinessIDPropagation(t *testing.T) {
	ns := startEmbeddedNATS(t)
	natsURL := ns.ClientURL()

	capturedBizID := make(chan string, 1)
	agentNC := connectNATS(t, natsURL)
	agent := a2a.NewAgent(a2a.AgentVK, a2a.NewNATSTransport(agentNC),
		a2a.HandlerFunc(func(_ context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
			capturedBizID <- req.BusinessID
			return &a2a.ToolResponse{TaskID: req.TaskID, Success: true, Result: map[string]interface{}{"ok": true}}, nil
		}),
	)
	require.NoError(t, agent.Start(context.Background()))

	orchNC := connectNATS(t, natsURL)
	conn := natsexec.NewNATSConn(orchNC)
	reg := tools.NewRegistry()
	reg.Register(llm.ToolDefinition{
		Type:     "function",
		Function: llm.FunctionDefinition{Name: "vk__publish_post", Description: "d", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
	}, natsexec.New(a2a.AgentVK, "vk__publish_post", conn))

	args, _ := json.Marshal(map[string]interface{}{})
	stub := &stubLLM{responses: []*llm.ChatResponse{
		{
			FinishReason: "tool_calls",
			ToolCalls:    []llm.ToolCall{{ID: "c1", Type: "function", Function: llm.FunctionCall{Name: "vk__publish_post", Arguments: string(args)}}},
		},
		{Content: "ok", FinishReason: "stop"},
	}}

	orch := orchestrator.New(stub, reg)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx = a2a.WithBusinessID(ctx, "biz-uuid-42")

	events, err := orch.Run(ctx, orchestrator.RunRequest{
		UserID:             uuid.New(),
		BusinessContext:    prompt.BusinessContext{Name: "T"},
		Messages:           []llm.Message{{Role: "user", Content: "x"}},
		ActiveIntegrations: []string{"vk"},
	})
	require.NoError(t, err)

	// Drain events
	for range events {
	}

	select {
	case bizID := <-capturedBizID:
		assert.Equal(t, "biz-uuid-42", bizID)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for agent to receive business ID")
	}
}
