package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	openai "github.com/sashabaranov/go-openai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/llm"
)

// chatCompletionResponse is the canned OpenAI-compatible response body for
// httptest servers that capture marshaled request bodies.
const chatCompletionResponse = `{
	"id": "chatcmpl-test",
	"object": "chat.completion",
	"choices": [{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
	"usage": {"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
}`

// openAICaptureServer is the httptest server pattern shared by the OpenAI,
// OpenRouter and SelfHosted SystemBlocks tests. It captures the marshaled
// /chat/completions request body into *dst.
func openAICaptureServer(t *testing.T, dst *[]byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*dst = b
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.Copy(w, strings.NewReader(chatCompletionResponse))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newOpenAIWithBase constructs an OpenAIProvider whose underlying client is
// pinned to baseURL — required for httptest capture in unit tests. The public
// NewOpenAI constructor accepts no base override (production never needs one),
// so this test seam reaches into the package-private field directly.
func newOpenAIWithBase(t *testing.T, baseURL string) *OpenAIProvider {
	t.Helper()
	cfg := openai.DefaultConfig("test-key")
	cfg.BaseURL = baseURL
	return &OpenAIProvider{client: openai.NewClientWithConfig(cfg)}
}

// newOpenRouterWithBase mirrors newOpenAIWithBase for the OpenRouter provider.
func newOpenRouterWithBase(t *testing.T, baseURL string) *OpenRouterProvider {
	t.Helper()
	cfg := openai.DefaultConfig("test-key")
	cfg.BaseURL = baseURL
	return &OpenRouterProvider{client: openai.NewClientWithConfig(cfg)}
}

// TestOpenAI_SystemBlocksConcatenated asserts that SystemBlocks prepends a
// single role:"system" message containing every block joined by "\n\n".
func TestOpenAI_SystemBlocksConcatenated(t *testing.T) {
	var captured []byte
	srv := openAICaptureServer(t, &captured)
	p := newOpenAIWithBase(t, srv.URL)

	_, err := p.Chat(context.Background(), llm.ChatRequest{
		Model: "gpt-4o-mini",
		SystemBlocks: []llm.SystemBlock{
			{Text: "P", CacheBoundary: true},
			{Text: "B"},
		},
		Messages: []llm.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(captured, &body), "body=%s", string(captured))
	msgs, ok := body["messages"].([]interface{})
	require.True(t, ok, "messages missing in body: %s", string(captured))
	require.GreaterOrEqual(t, len(msgs), 2, "expected leading system + user message: %s", string(captured))
	first := msgs[0].(map[string]interface{})
	assert.Equal(t, "system", first["role"])
	assert.Equal(t, "P\n\nB", first["content"])
	// User message preserved unchanged in second position.
	assert.Equal(t, "user", msgs[1].(map[string]interface{})["role"])
}

// TestOpenAI_SystemBlocksEmpty_LegacyMessagesPath asserts that an empty
// SystemBlocks slice leaves Messages untouched (back-compat path for
// non-migrated callers such as titler / draft_reply).
func TestOpenAI_SystemBlocksEmpty_LegacyMessagesPath(t *testing.T) {
	var captured []byte
	srv := openAICaptureServer(t, &captured)
	p := newOpenAIWithBase(t, srv.URL)

	_, err := p.Chat(context.Background(), llm.ChatRequest{
		Model: "gpt-4o-mini",
		Messages: []llm.Message{
			{Role: "system", Content: "legacy"},
			{Role: "user", Content: "hi"},
		},
	})
	require.NoError(t, err)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(captured, &body))
	msgs := body["messages"].([]interface{})
	require.Len(t, msgs, 2)
	assert.Equal(t, "system", msgs[0].(map[string]interface{})["role"])
	assert.Equal(t, "legacy", msgs[0].(map[string]interface{})["content"])
}

// TestOpenRouter_SystemBlocksConcatenated mirrors the OpenAI test for the
// OpenRouter provider — both wrap the same go-openai client type, so the
// projection logic must stay byte-identical.
func TestOpenRouter_SystemBlocksConcatenated(t *testing.T) {
	var captured []byte
	srv := openAICaptureServer(t, &captured)
	p := newOpenRouterWithBase(t, srv.URL)

	_, err := p.Chat(context.Background(), llm.ChatRequest{
		Model: "openai/gpt-4o-mini",
		SystemBlocks: []llm.SystemBlock{
			{Text: "alpha", CacheBoundary: true},
			{Text: "beta"},
		},
		Messages: []llm.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(captured, &body))
	msgs := body["messages"].([]interface{})
	require.GreaterOrEqual(t, len(msgs), 2)
	first := msgs[0].(map[string]interface{})
	assert.Equal(t, "system", first["role"])
	assert.Equal(t, "alpha\n\nbeta", first["content"])
}

// TestSelfHosted_SystemBlocksConcatenated mirrors the OpenAI test for the
// SelfHosted provider. NewSelfHosted accepts a baseURL so no internal seam
// is required.
func TestSelfHosted_SystemBlocksConcatenated(t *testing.T) {
	var captured []byte
	srv := openAICaptureServer(t, &captured)

	p := NewSelfHosted("selfhosted-0", srv.URL+"/v1", "")
	require.NotNil(t, p)

	_, err := p.Chat(context.Background(), llm.ChatRequest{
		Model: "llama3.1",
		SystemBlocks: []llm.SystemBlock{
			{Text: "rules", CacheBoundary: true},
			{Text: "biz"},
		},
		Messages: []llm.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(captured, &body))
	msgs := body["messages"].([]interface{})
	require.GreaterOrEqual(t, len(msgs), 2)
	first := msgs[0].(map[string]interface{})
	assert.Equal(t, "system", first["role"])
	assert.Equal(t, "rules\n\nbiz", first["content"])
}

// TestAnthropic_SystemBlocksPreferredOverScrub asserts that when both
// SystemBlocks and a legacy role:"system" Messages entry are present, the
// SystemBlocks channel wins — the role:"system" entry is treated as a wiring
// bug rather than re-emitted (Plan 24-02 contract).
func TestAnthropic_SystemBlocksPreferredOverScrub(t *testing.T) {
	var captured []byte
	srv := captureBodyServer(t, &captured, minimalMessageResponse("claude-haiku-4-5", "end_turn"))
	p := newAnthropicWithBase(t, srv.URL)

	_, err := p.Chat(context.Background(), llm.ChatRequest{
		Model: "claude-haiku-4-5",
		SystemBlocks: []llm.SystemBlock{
			{Text: "A", CacheBoundary: true},
			{Text: "B"},
		},
		Messages: []llm.Message{
			{Role: "user", Content: "hi"},
		},
	})
	require.NoError(t, err)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(captured, &body), "body=%s", string(captured))
	systemArr, ok := body["system"].([]interface{})
	require.True(t, ok, "system missing: %s", string(captured))
	require.Len(t, systemArr, 2, "system blocks must come from req.SystemBlocks, not from Messages")
	assert.Equal(t, "A", systemArr[0].(map[string]interface{})["text"])
	assert.Equal(t, "B", systemArr[1].(map[string]interface{})["text"])

	// Block A flagged CacheBoundary → carries cache_control. Block B does not.
	ccA, hasA := systemArr[0].(map[string]interface{})["cache_control"]
	require.True(t, hasA, "CacheBoundary=true block must carry cache_control: %v", systemArr[0])
	assert.Equal(t, "ephemeral", ccA.(map[string]interface{})["type"])
	_, hasB := systemArr[1].(map[string]interface{})["cache_control"]
	assert.False(t, hasB, "non-CacheBoundary block must NOT carry cache_control: %v", systemArr[1])
}

// TestAnthropic_CacheBoundaryStampsLastFlagged proves that when multiple
// blocks carry CacheBoundary=true, the LAST such block gets cache_control —
// even when a non-flagged block follows. Plan 24-02 RESEARCH §Pattern 2:
// "Anthropic stamps cache_control on the LAST block marked CacheBoundary=true."
func TestAnthropic_CacheBoundaryStampsLastFlagged(t *testing.T) {
	var captured []byte
	srv := captureBodyServer(t, &captured, minimalMessageResponse("claude-haiku-4-5", "end_turn"))
	p := newAnthropicWithBase(t, srv.URL)

	_, err := p.Chat(context.Background(), llm.ChatRequest{
		Model: "claude-haiku-4-5",
		SystemBlocks: []llm.SystemBlock{
			{Text: "A", CacheBoundary: true},
			{Text: "B", CacheBoundary: false},
			{Text: "C"},
		},
		Messages: []llm.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(captured, &body))
	systemArr := body["system"].([]interface{})
	require.Len(t, systemArr, 3)

	cc0, has0 := systemArr[0].(map[string]interface{})["cache_control"]
	require.True(t, has0, "Block A (last CacheBoundary=true) must carry cache_control")
	assert.Equal(t, "ephemeral", cc0.(map[string]interface{})["type"])
	_, has1 := systemArr[1].(map[string]interface{})["cache_control"]
	assert.False(t, has1, "Block B (CacheBoundary=false) must NOT carry cache_control")
	_, has2 := systemArr[2].(map[string]interface{})["cache_control"]
	assert.False(t, has2, "Block C (no flag) must NOT carry cache_control")
}

// TestAnthropic_LegacyScrubFallback asserts that when SystemBlocks is empty,
// the legacy role:"system" scrub path is preserved with Plan 24-01's
// cache_control on the last scrubbed block. This is the back-compat fallback
// for non-migrated callers (titler, draft_reply).
func TestAnthropic_LegacyScrubFallback(t *testing.T) {
	var captured []byte
	srv := captureBodyServer(t, &captured, minimalMessageResponse("claude-haiku-4-5", "end_turn"))
	p := newAnthropicWithBase(t, srv.URL)

	_, err := p.Chat(context.Background(), llm.ChatRequest{
		Model: "claude-haiku-4-5",
		Messages: []llm.Message{
			{Role: "system", Content: "X"},
			{Role: "user", Content: "hi"},
		},
	})
	require.NoError(t, err)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(captured, &body))
	systemArr := body["system"].([]interface{})
	require.Len(t, systemArr, 1)
	assert.Equal(t, "X", systemArr[0].(map[string]interface{})["text"])
	cc, has := systemArr[0].(map[string]interface{})["cache_control"]
	require.True(t, has, "legacy scrub path must keep Plan 24-01 cache_control")
	assert.Equal(t, "ephemeral", cc.(map[string]interface{})["type"])
}
