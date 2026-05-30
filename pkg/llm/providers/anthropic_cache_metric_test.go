package providers

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/llm"
)

// cacheReadCounter looks up the current value of
// `llm_cache_read_tokens_total{model=<model>}`. Returns 0 if absent.
func cacheReadCounter(t *testing.T, model string) float64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, mf := range families {
		if mf.GetName() != "llm_cache_read_tokens_total" {
			continue
		}
		for _, m := range mf.Metric {
			if labelValue(m, "model") == model {
				return m.GetCounter().GetValue()
			}
		}
	}
	return 0
}

func labelValue(m *dto.Metric, name string) string {
	for _, l := range m.Label {
		if l.GetName() == name {
			return l.GetValue()
		}
	}
	return ""
}

func TestAnthropic_UsagePopulatesCache(t *testing.T) {
	model := "claude-haiku-4-5"
	respBody := `{
		"id": "msg_cache",
		"type": "message",
		"role": "assistant",
		"model": "` + model + `",
		"content": [{"type": "text", "text": "ok"}],
		"stop_reason": "end_turn",
		"stop_sequence": "",
		"usage": {
			"cache_creation": {"ephemeral_5m_input_tokens": 25, "ephemeral_1h_input_tokens": 0},
			"cache_creation_input_tokens": 25,
			"cache_read_input_tokens": 50,
			"inference_geo": "us",
			"input_tokens": 100,
			"output_tokens": 20,
			"server_tool_use": {"web_search_requests": 0},
			"service_tier": "standard"
		}
	}`
	var captured []byte
	srv := captureBodyServer(t, &captured, respBody)
	p := newAnthropicWithBase(t, srv.URL)

	base := cacheReadCounter(t, model)

	resp, err := p.Chat(context.Background(), llm.ChatRequest{
		Model:    model,
		Messages: []llm.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	assert.Equal(t, 50, resp.Usage.CacheReadTokens)
	assert.Equal(t, 25, resp.Usage.CacheCreationTokens)
	assert.Equal(t, 100, resp.Usage.InputTokens)
	assert.Equal(t, 20, resp.Usage.OutputTokens)

	got := cacheReadCounter(t, model)
	assert.Equal(t, base+50, got, "llm_cache_read_tokens_total should have increased by 50")
}
