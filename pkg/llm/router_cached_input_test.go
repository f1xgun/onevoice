package llm_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/llm/providers"
)

// cachedUsageCompletionBody is an OpenAI-compatible completion whose usage
// reports a prompt cache: prompt_tokens counts the WHOLE prompt (1000), of
// which prompt_tokens_details.cached_tokens (800) were served from cache. A
// self-hosted vLLM with prefix caching or gpt-5-mini emits exactly this shape.
const cachedUsageCompletionBody = `{
	"id": "chatcmpl-cache",
	"object": "chat.completion",
	"choices": [{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
	"usage": {"prompt_tokens":1000,"completion_tokens":200,"total_tokens":1200,"prompt_tokens_details":{"cached_tokens":800}}
}`

func fixedCompletionServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.Copy(w, strings.NewReader(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestRouter_CachedTokens_DiscountsBillingInput drives a real OpenAI-compatible
// provider (self-hosted) through the Router end-to-end. The upstream reports
// prompt_tokens_details.cached_tokens; the adapter splits the cached prefix into
// CacheReadTokens, so the router bills it at the cache rate (0.1x) instead of
// the full input rate. This is the lever: the per-business daily-spend gate,
// which accumulates provider cost, stops charging the cached prefix at full
// price and no longer fires early.
func TestRouter_CachedTokens_DiscountsBillingInput(t *testing.T) {
	srv := fixedCompletionServer(t, cachedUsageCompletionBody)

	const provider = "selfhosted-cache"
	p := providers.NewSelfHosted(provider, srv.URL+"/v1", "")
	require.NotNil(t, p)

	const inputCost, outputCost = 3.0, 15.0
	entry := healthyEntry("local-mini", provider, inputCost, outputCost, 300)
	registry := newTestRegistry(entry)

	billing := &MockBillingRepository{}
	frl := &fakeRateLimiter{allowed: true, recordCh: make(chan int, 1)}
	r := llm.NewRouter(registry,
		llm.WithProvider(p),
		llm.WithBilling(billing),
		llm.WithRateLimitChecker(frl),
	)

	_, err := r.Chat(context.Background(), llm.ChatRequest{
		Model:      "local-mini",
		UserID:     uuid.New(),
		BusinessID: uuid.New(),
		Tier:       "free",
		Messages:   []llm.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)

	assert.Eventually(t, func() bool { return billing.LastLog() != nil },
		time.Second, 10*time.Millisecond, "billing log should appear")

	last := billing.LastLog()
	require.NotNil(t, last)
	assert.Equal(t, 200, last.InputTokens, "billed InputTokens is the post-cache remainder 1000-800")
	assert.Equal(t, 800, last.CacheReadTokens, "the cached prefix must be attributed to CacheReadTokens")
	assert.Equal(t, 0, last.CacheCreationTokens)

	const discounted = (200.0 + 800.0*0.1) * inputCost / 1_000_000.0
	const outputUSD = 200.0 * outputCost / 1_000_000.0
	assert.InDelta(t, discounted+outputUSD, last.ProviderCostUSD, 1e-9,
		"cached prefix must be billed at 0.1x, not the full input rate")

	const fullRateInput = 1000.0 * inputCost / 1_000_000.0
	assert.Less(t, last.ProviderCostUSD, fullRateInput+outputUSD,
		"the fix must reduce provider cost versus billing the whole prompt at the full input rate")

	select {
	case delta := <-frl.recordCh:
		assert.Equal(t, 1000, last.InputTokens+last.CacheReadTokens,
			"no tokens are dropped from the count — Input+CacheRead still sums to prompt_tokens")
		assert.Greater(t, delta, 1000,
			"the token-count reconcile still charges the full processed volume (Input+Output+CacheRead), unchanged by caching")
		assert.LessOrEqual(t, delta, 1200)
	case <-time.After(2 * time.Second):
		t.Fatal("reconcile RecordTokens was not called within timeout")
	}
}
