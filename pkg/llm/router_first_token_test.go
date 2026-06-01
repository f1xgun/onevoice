package llm_test

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/llm"
)

// streamingStubProvider emits a fixed sequence of chunks with a configurable
// delay before the first one. Used by TestFirstTokenLatency to verify the
// router observes the first-chunk moment exactly once per ChatStream call.
type streamingStubProvider struct {
	name          string
	chunks        []string
	firstDelay    time.Duration
	betweenChunks time.Duration
}

func (p *streamingStubProvider) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{Content: "unused"}, nil
}

func (p *streamingStubProvider) ChatStream(_ context.Context, _ llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	out := make(chan llm.StreamChunk, len(p.chunks))
	go func() {
		defer close(out)
		for i, c := range p.chunks {
			if i == 0 {
				time.Sleep(p.firstDelay)
			} else {
				time.Sleep(p.betweenChunks)
			}
			out <- llm.StreamChunk{Delta: c}
		}
	}()
	return out, nil
}

func (p *streamingStubProvider) ListModels(_ context.Context) ([]llm.ModelInfo, error) {
	return nil, nil
}
func (p *streamingStubProvider) HealthCheck(_ context.Context) error { return nil }
func (p *streamingStubProvider) Name() string                        { return p.name }

// firstTokenName is unique to this test so the histogram observation can be
// asserted in isolation from any other ChatStream test in the package.
const firstTokenName = "first-token-stub-model"

func TestFirstTokenLatency_RecordsExactlyOnceOnFirstChunk(t *testing.T) {
	entry := &llm.ModelProviderEntry{Model: firstTokenName, Provider: "stub-provider"}
	prov := &streamingStubProvider{
		name:          "stub-provider",
		chunks:        []string{"hello", " ", "world"},
		firstDelay:    5 * time.Millisecond,
		betweenChunks: 1 * time.Millisecond,
	}
	sel := &candidateSelector{candidates: []llm.Candidate{{Entry: entry, Provider: prov}}}
	r := llm.NewRouter(llm.NewRegistry(), llm.WithSelector(sel))

	wantLabels := map[string]string{
		"model":    firstTokenName,
		"provider": "stub-provider",
	}
	before := firstTokenHistogramCount(t, wantLabels)

	ch, err := r.ChatStream(context.Background(), llm.ChatRequest{Model: firstTokenName})
	require.NoError(t, err)
	require.NotNil(t, ch)

	// Drain the channel so the wrapper goroutine completes.
	chunksReceived := 0
	for range ch {
		chunksReceived++
	}
	assert.Equal(t, 3, chunksReceived, "all 3 chunks must reach the caller")

	after := firstTokenHistogramCount(t, wantLabels)
	assert.Equal(t, before+1, after,
		"llm_first_token_latency_seconds must record exactly one observation per ChatStream call")
}

// firstTokenHistogramCount returns the sample count of the
// llm_first_token_latency_seconds histogram for the given labels.
// Returns 0 if the metric / label set has not been observed yet.
func firstTokenHistogramCount(t *testing.T, want map[string]string) uint64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, mf := range families {
		if mf.GetName() != "llm_first_token_latency_seconds" {
			continue
		}
		for _, m := range mf.GetMetric() {
			if !labelSubsetMatches(m.GetLabel(), want) {
				continue
			}
			h := m.GetHistogram()
			if h == nil {
				return 0
			}
			return h.GetSampleCount()
		}
	}
	return 0
}

func labelSubsetMatches(have []*dto.LabelPair, want map[string]string) bool {
	for k, v := range want {
		found := false
		for _, lp := range have {
			if lp.GetName() == k && lp.GetValue() == v {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
