package providers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/llm"
)

// chatCompletionToolCallStopResponse models the misbehaving upstream: a
// completion that carries tool_calls while reporting finish_reason="stop"
// (observed on free/quantized models proxied via OpenRouter). The adapter must
// normalize FinishReason to "tool_calls" so downstream consumers never mistake
// a tool turn for a terminal text turn.
const chatCompletionToolCallStopResponse = `{
	"id": "chatcmpl-test",
	"object": "chat.completion",
	"choices": [{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"telegram__send_channel_post","arguments":"{}"}}]}}],
	"usage": {"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
}`

// openAIFixedResponseServer serves a fixed canned body for every request.
func openAIFixedResponseServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.Copy(w, strings.NewReader(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestOpenAICompat_ToolCallsWithStopFinish_NormalizesFinishReason asserts the
// defense-in-depth normalization: when the upstream returns tool_calls with
// finish_reason="stop", the adapter rewrites FinishReason to "tool_calls" so
// the FinishReason contract matches the Anthropic adapter (tool_use→tool_calls)
// and the orchestrator cannot mis-route the turn to its terminal branch.
func TestOpenAICompat_ToolCallsWithStopFinish_NormalizesFinishReason(t *testing.T) {
	srv := openAIFixedResponseServer(t, chatCompletionToolCallStopResponse)
	p := newOpenRouterWithBase(t, srv.URL)

	resp, err := p.Chat(context.Background(), llm.ChatRequest{
		Model:    "free/quantized-model",
		Messages: []llm.Message{{Role: "user", Content: "post it"}},
	})
	require.NoError(t, err)
	require.Len(t, resp.ToolCalls, 1, "tool call must be parsed from the upstream response")
	assert.Equal(t, "tool_calls", resp.FinishReason,
		"FinishReason MUST be normalized to tool_calls when the completion carries tool_calls, even if the upstream reported stop")
}

// TestOpenAICompat_NoToolCalls_PreservesStopFinish guards the inverse: a plain
// text completion with finish_reason="stop" and no tool_calls must keep its
// "stop" FinishReason untouched.
func TestOpenAICompat_NoToolCalls_PreservesStopFinish(t *testing.T) {
	srv := openAIFixedResponseServer(t, chatCompletionResponse)
	p := newOpenRouterWithBase(t, srv.URL)

	resp, err := p.Chat(context.Background(), llm.ChatRequest{
		Model:    "free/quantized-model",
		Messages: []llm.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	require.Empty(t, resp.ToolCalls)
	assert.Equal(t, "stop", resp.FinishReason,
		"a tool-free completion must keep its original finish_reason")
}
