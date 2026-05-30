package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/llm"
)

// minimalMessageResponse returns the JSON body for a synthetic Anthropic
// Messages.New response. Cache fields default to zero unless overridden by
// callers via the optional `usageOverride` argument.
func minimalMessageResponse(model, stopReason string) string {
	return `{
		"id": "msg_test",
		"type": "message",
		"role": "assistant",
		"model": "` + model + `",
		"content": [{"type": "text", "text": "ok"}],
		"stop_reason": "` + stopReason + `",
		"stop_sequence": "",
		"usage": {
			"cache_creation": {"ephemeral_5m_input_tokens": 0, "ephemeral_1h_input_tokens": 0},
			"cache_creation_input_tokens": 0,
			"cache_read_input_tokens": 0,
			"inference_geo": "us",
			"input_tokens": 10,
			"output_tokens": 5,
			"server_tool_use": {"web_search_requests": 0},
			"service_tier": "standard"
		}
	}`
}

// captureBodyServer returns an httptest.Server that captures the request body
// into `dst` and responds with `respBody`.
func captureBodyServer(t *testing.T, dst *[]byte, respBody string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*dst = b
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.Copy(w, strings.NewReader(respBody))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newAnthropicWithBase constructs a provider whose SDK client is pinned to the
// given base URL — required for httptest capture in unit tests.
func newAnthropicWithBase(t *testing.T, baseURL string) *AnthropicProvider {
	t.Helper()
	client := anthropic.NewClient(
		option.WithAPIKey("test-key"),
		option.WithBaseURL(baseURL+"/"),
	)
	return &AnthropicProvider{client: &client}
}

func TestAnthropic_ToolTranslation(t *testing.T) {
	t.Run("empty input returns nil", func(t *testing.T) {
		got := toolsToAnthropic(nil)
		assert.Nil(t, got)

		got = toolsToAnthropic([]llm.ToolDefinition{})
		assert.Nil(t, got)
	})

	t.Run("single tool gets cache_control on last entry", func(t *testing.T) {
		in := []llm.ToolDefinition{{
			Type: llm.ToolCallTypeFunction,
			Function: llm.FunctionDefinition{
				Name:        "x",
				Description: "does x",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"x": map[string]interface{}{"type": "string"},
					},
					"required": []string{"x"},
				},
			},
		}}
		out := toolsToAnthropic(in)
		require.Len(t, out, 1)
		require.NotNil(t, out[0].OfTool)
		assert.Equal(t, "x", out[0].OfTool.Name)
		assert.Equal(t, "does x", out[0].OfTool.Description.Value)
		assert.Equal(t, []string{"x"}, out[0].OfTool.InputSchema.Required)
		assert.Equal(t, "ephemeral", string(out[0].OfTool.CacheControl.Type))
	})

	t.Run("only last of multiple tools has cache_control", func(t *testing.T) {
		in := []llm.ToolDefinition{
			{
				Type: llm.ToolCallTypeFunction,
				Function: llm.FunctionDefinition{
					Name:       "first",
					Parameters: map[string]interface{}{"properties": map[string]interface{}{}},
				},
			},
			{
				Type: llm.ToolCallTypeFunction,
				Function: llm.FunctionDefinition{
					Name:       "last",
					Parameters: map[string]interface{}{"properties": map[string]interface{}{}},
				},
			},
		}
		out := toolsToAnthropic(in)
		require.Len(t, out, 2)
		require.NotNil(t, out[0].OfTool)
		require.NotNil(t, out[1].OfTool)
		assert.Empty(t, string(out[0].OfTool.CacheControl.Type), "first tool must NOT carry cache_control")
		assert.Equal(t, "ephemeral", string(out[1].OfTool.CacheControl.Type), "last tool MUST carry cache_control")
	})
}

func TestAnthropic_StopReasonMapping(t *testing.T) {
	cases := []struct {
		in   anthropic.StopReason
		want string
	}{
		{anthropic.StopReasonToolUse, "tool_calls"},
		{anthropic.StopReasonEndTurn, "stop"},
		{anthropic.StopReasonMaxTokens, "length"},
		{anthropic.StopReasonStopSequence, "stop"},
		{anthropic.StopReasonPauseTurn, "stop"},
		{anthropic.StopReasonRefusal, "stop"},
		{anthropic.StopReason("future_unknown"), "future_unknown"},
	}
	for _, tc := range cases {
		t.Run(string(tc.in), func(t *testing.T) {
			assert.Equal(t, tc.want, mapStopReason(tc.in))
		})
	}
}

func TestAnthropic_SystemRouting(t *testing.T) {
	t.Run("two system blocks plus user message", func(t *testing.T) {
		req := llm.ChatRequest{
			Messages: []llm.Message{
				{Role: "system", Content: "A"},
				{Role: "system", Content: "B"},
				{Role: "user", Content: "hi"},
			},
		}
		systemBlocks, msgs := buildAnthropicMessagesV2(req)
		require.Len(t, systemBlocks, 2)
		assert.Equal(t, "A", systemBlocks[0].Text)
		assert.Equal(t, "B", systemBlocks[1].Text)
		require.Len(t, msgs, 1)
		assert.Equal(t, anthropic.MessageParamRoleUser, msgs[0].Role)
	})

	t.Run("no system messages yields empty system slice", func(t *testing.T) {
		req := llm.ChatRequest{
			Messages: []llm.Message{{Role: "user", Content: "hi"}},
		}
		systemBlocks, msgs := buildAnthropicMessagesV2(req)
		assert.Empty(t, systemBlocks)
		require.Len(t, msgs, 1)
	})

	t.Run("assistant tool_calls and tool result are projected", func(t *testing.T) {
		req := llm.ChatRequest{
			Messages: []llm.Message{
				{Role: "user", Content: "do thing"},
				{
					Role:    "assistant",
					Content: "calling tool",
					ToolCalls: []llm.ToolCall{{
						ID:   "call_1",
						Type: llm.ToolCallTypeFunction,
						Function: llm.FunctionCall{
							Name:      "telegram__send_channel_post",
							Arguments: `{"channel_id":"abc"}`,
						},
					}},
				},
				{Role: "tool", Content: `{"ok":true}`, ToolCallID: "call_1"},
			},
		}
		systemBlocks, msgs := buildAnthropicMessagesV2(req)
		assert.Empty(t, systemBlocks)
		require.Len(t, msgs, 3)

		// user
		assert.Equal(t, anthropic.MessageParamRoleUser, msgs[0].Role)

		// assistant with text + tool_use block
		assert.Equal(t, anthropic.MessageParamRoleAssistant, msgs[1].Role)
		require.GreaterOrEqual(t, len(msgs[1].Content), 1)
		// At least one block must be a tool_use referencing call_1.
		foundToolUse := false
		for _, blk := range msgs[1].Content {
			if blk.OfToolUse != nil && blk.OfToolUse.ID == "call_1" && blk.OfToolUse.Name == "telegram__send_channel_post" {
				foundToolUse = true
			}
		}
		assert.True(t, foundToolUse, "assistant message must carry a tool_use block matching the ToolCall")

		// tool message as user-role MessageParam with tool_result block
		assert.Equal(t, anthropic.MessageParamRoleUser, msgs[2].Role)
		require.Len(t, msgs[2].Content, 1)
		require.NotNil(t, msgs[2].Content[0].OfToolResult)
		assert.Equal(t, "call_1", msgs[2].Content[0].OfToolResult.ToolUseID)
	})
}

func TestAnthropic_DefaultMaxTokens(t *testing.T) {
	cases := []struct {
		name      string
		model     string
		callerMax int
		wantMax   int64
	}{
		{"haiku zero falls back to 4096", "claude-haiku-4-5", 0, 4096},
		{"sonnet zero falls back to 8192", "claude-sonnet-4-6", 0, 8192},
		{"caller override is preserved", "claude-haiku-4-5", 1024, 1024},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var captured []byte
			srv := captureBodyServer(t, &captured, minimalMessageResponse(tc.model, "end_turn"))
			p := newAnthropicWithBase(t, srv.URL)

			_, err := p.Chat(context.Background(), llm.ChatRequest{
				UserID:    uuid.New(),
				Model:     tc.model,
				MaxTokens: tc.callerMax,
				Messages:  []llm.Message{{Role: "user", Content: "hi"}},
			})
			require.NoError(t, err)

			var body map[string]interface{}
			require.NoError(t, json.Unmarshal(captured, &body), "body=%s", string(captured))
			// JSON numbers decode as float64.
			require.IsType(t, float64(0), body["max_tokens"])
			assert.Equal(t, tc.wantMax, int64(body["max_tokens"].(float64)))
		})
	}
}

func TestAnthropic_CacheControlPlacement(t *testing.T) {
	t.Run("cache_control stamped on last system block", func(t *testing.T) {
		var captured []byte
		srv := captureBodyServer(t, &captured, minimalMessageResponse("claude-haiku-4-5", "end_turn"))
		p := newAnthropicWithBase(t, srv.URL)

		_, err := p.Chat(context.Background(), llm.ChatRequest{
			Model: "claude-haiku-4-5",
			Messages: []llm.Message{
				{Role: "system", Content: "platform rules"},
				{Role: "user", Content: "hi"},
			},
		})
		require.NoError(t, err)

		var body map[string]interface{}
		require.NoError(t, json.Unmarshal(captured, &body), "body=%s", string(captured))
		systemAny, ok := body["system"]
		require.True(t, ok, "system field missing: %s", string(captured))
		systemArr, ok := systemAny.([]interface{})
		require.True(t, ok)
		require.Len(t, systemArr, 1)
		blk := systemArr[0].(map[string]interface{})
		cc, ok := blk["cache_control"].(map[string]interface{})
		require.True(t, ok, "last system block must carry cache_control: %v", blk)
		assert.Equal(t, "ephemeral", cc["type"])
	})

	t.Run("cache_control only on last tool entry", func(t *testing.T) {
		var captured []byte
		srv := captureBodyServer(t, &captured, minimalMessageResponse("claude-haiku-4-5", "end_turn"))
		p := newAnthropicWithBase(t, srv.URL)

		tools := []llm.ToolDefinition{
			{
				Type: llm.ToolCallTypeFunction,
				Function: llm.FunctionDefinition{
					Name:        "first_tool",
					Description: "first",
					Parameters: map[string]interface{}{
						"type":       "object",
						"properties": map[string]interface{}{},
					},
				},
			},
			{
				Type: llm.ToolCallTypeFunction,
				Function: llm.FunctionDefinition{
					Name:        "last_tool",
					Description: "last",
					Parameters: map[string]interface{}{
						"type":       "object",
						"properties": map[string]interface{}{},
					},
				},
			},
		}
		_, err := p.Chat(context.Background(), llm.ChatRequest{
			Model:    "claude-haiku-4-5",
			Messages: []llm.Message{{Role: "user", Content: "hi"}},
			Tools:    tools,
		})
		require.NoError(t, err)

		var body map[string]interface{}
		require.NoError(t, json.Unmarshal(captured, &body), "body=%s", string(captured))
		toolsAny, ok := body["tools"]
		require.True(t, ok, "tools missing in body: %s", string(captured))
		toolsArr := toolsAny.([]interface{})
		require.Len(t, toolsArr, 2)

		first := toolsArr[0].(map[string]interface{})
		last := toolsArr[1].(map[string]interface{})

		_, firstHasCC := first["cache_control"]
		assert.False(t, firstHasCC, "first tool must NOT carry cache_control: %v", first)

		ccAny, ok := last["cache_control"].(map[string]interface{})
		require.True(t, ok, "last tool must carry cache_control: %v", last)
		assert.Equal(t, "ephemeral", ccAny["type"])
	})
}
