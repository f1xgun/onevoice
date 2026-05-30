package providers

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultMaxTokensFor_KnownAndUnknownModels(t *testing.T) {
	cases := []struct {
		model string
		want  int64
	}{
		{"claude-sonnet-4-6", 8192},
		{"claude-haiku-4-5", 4096},
		{"claude-haiku-4-5-20251001", 4096},
		{"claude-opus-4-7", 8192},
		{"claude-opus-4-6", 8192},
		{"claude-sonnet-4-5", 8192},
		{"claude-sonnet-4-5-20250929", 8192},
		{"some-future-model", 4096},
		{"", 4096},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			assert.Equal(t, tc.want, defaultMaxTokensFor(tc.model))
		})
	}
}

func TestAnthropic_HealthCheckModel(t *testing.T) {
	var captured []byte
	srv := captureBodyServer(t, &captured, minimalMessageResponse("claude-haiku-4-5", "end_turn"))
	p := newAnthropicWithBase(t, srv.URL)

	require.NoError(t, p.HealthCheck(context.Background()))

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(captured, &body), "body=%s", string(captured))
	assert.Equal(t, "claude-haiku-4-5", body["model"], "HealthCheck must ping claude-haiku-4-5 (Sonnet/Opus 4 family deprecation 2026-06-15)")
}

func TestAnthropic_ListModels_Current(t *testing.T) {
	p := newAnthropicWithBase(t, "http://unused.example")

	models, err := p.ListModels(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, models)

	byID := make(map[string]int, len(models))
	for i, m := range models {
		byID[m.ID] = i
	}

	// Required entries.
	require.Contains(t, byID, "claude-sonnet-4-6")
	require.Contains(t, byID, "claude-haiku-4-5")
	require.Contains(t, byID, "claude-opus-4-7")

	sonnet := models[byID["claude-sonnet-4-6"]]
	haiku := models[byID["claude-haiku-4-5"]]
	opus := models[byID["claude-opus-4-7"]]

	require.NotNil(t, sonnet.InputCostPer1MTok)
	require.NotNil(t, sonnet.OutputCostPer1MTok)
	assert.InDelta(t, 3.0, *sonnet.InputCostPer1MTok, 0.0001)
	assert.InDelta(t, 15.0, *sonnet.OutputCostPer1MTok, 0.0001)
	assert.Equal(t, claudeSonnet4_6ContextLength, sonnet.ContextLength)

	require.NotNil(t, haiku.InputCostPer1MTok)
	require.NotNil(t, haiku.OutputCostPer1MTok)
	assert.InDelta(t, 1.0, *haiku.InputCostPer1MTok, 0.0001)
	assert.InDelta(t, 5.0, *haiku.OutputCostPer1MTok, 0.0001)
	assert.Equal(t, claudeHaiku4_5ContextLength, haiku.ContextLength)

	require.NotNil(t, opus.InputCostPer1MTok)
	require.NotNil(t, opus.OutputCostPer1MTok)
	assert.InDelta(t, 5.0, *opus.InputCostPer1MTok, 0.0001)
	assert.InDelta(t, 25.0, *opus.OutputCostPer1MTok, 0.0001)
	assert.Equal(t, claudeOpus4_7ContextLength, opus.ContextLength)

	// Regression guard: obsolete 3.5 entries must not reappear.
	assert.NotContains(t, byID, "claude-3-5-sonnet-20241022")
	assert.NotContains(t, byID, "claude-3-5-haiku-20241022")
}
