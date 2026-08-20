package providers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/llm"
)

// chatCompletionCachedUsageResponse models an OpenAI-compatible upstream that
// applied a prompt cache: prompt_tokens counts the WHOLE prompt (1000), of
// which prompt_tokens_details.cached_tokens (800) were served from cache.
const chatCompletionCachedUsageResponse = `{
	"id": "chatcmpl-test",
	"object": "chat.completion",
	"choices": [{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
	"usage": {"prompt_tokens":1000,"completion_tokens":200,"total_tokens":1200,"prompt_tokens_details":{"cached_tokens":800}}
}`

// chatCompletionZeroCachedUsageResponse carries the details object but reports
// zero cached tokens — the common cold-cache turn. Must behave exactly like the
// no-details response (whole prompt billed as input, no cache-read split).
const chatCompletionZeroCachedUsageResponse = `{
	"id": "chatcmpl-test",
	"object": "chat.completion",
	"choices": [{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
	"usage": {"prompt_tokens":1000,"completion_tokens":200,"total_tokens":1200,"prompt_tokens_details":{"cached_tokens":0}}
}`

// chatCompletionOvercachedUsageResponse reports more cached tokens than the
// whole prompt — a malformed upstream. The mapping must clamp so InputTokens
// never goes negative.
const chatCompletionOvercachedUsageResponse = `{
	"id": "chatcmpl-test",
	"object": "chat.completion",
	"choices": [{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
	"usage": {"prompt_tokens":1000,"completion_tokens":200,"total_tokens":1200,"prompt_tokens_details":{"cached_tokens":4000}}
}`

// TestOpenAICompat_Usage_MapsCachedTokens proves the adapter reads
// prompt_tokens_details.cached_tokens and splits it out of InputTokens into
// CacheReadTokens — so the router's cache-aware billing and the per-business
// daily-spend gate stop charging the cached prefix at the full input rate.
func TestOpenAICompat_Usage_MapsCachedTokens(t *testing.T) {
	srv := openAIFixedResponseServer(t, chatCompletionCachedUsageResponse)
	p := newOpenRouterWithBase(t, srv.URL)

	resp, err := p.Chat(context.Background(), llm.ChatRequest{
		Model:    "gpt-5-mini",
		Messages: []llm.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)

	assert.Equal(t, 200, resp.Usage.InputTokens,
		"InputTokens must be the post-cache remainder prompt_tokens(1000) - cached(800)")
	assert.Equal(t, 800, resp.Usage.CacheReadTokens,
		"CacheReadTokens must carry the cached prefix so billing discounts it at the cache rate")
	assert.Equal(t, 0, resp.Usage.CacheCreationTokens,
		"the OpenAI-compatible surface bills no separate cache-write class")
	assert.Equal(t, 1200, resp.Usage.TotalTokens,
		"TotalTokens is untouched — the model still processed the full prompt + completion")
	assert.Equal(t, 200, resp.Usage.OutputTokens)

	assert.Equal(t, resp.Usage.InputTokens+resp.Usage.CacheReadTokens, 1000,
		"InputTokens + CacheReadTokens must still sum to the reported prompt_tokens — no tokens are dropped from the count")
}

// TestOpenAICompat_Usage_NoCachedTokens_LegacyBehavior proves an upstream that
// omits prompt_tokens_details leaves the mapping unchanged: whole prompt as
// InputTokens, CacheReadTokens zero. This is the no-regression guard for
// providers with no cache surface.
func TestOpenAICompat_Usage_NoCachedTokens_LegacyBehavior(t *testing.T) {
	srv := openAIFixedResponseServer(t, chatCompletionResponse)
	p := newOpenRouterWithBase(t, srv.URL)

	resp, err := p.Chat(context.Background(), llm.ChatRequest{
		Model:    "gpt-4o-mini",
		Messages: []llm.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)

	assert.Equal(t, 1, resp.Usage.InputTokens, "whole prompt_tokens billed as input when no cache is reported")
	assert.Equal(t, 0, resp.Usage.CacheReadTokens)
	assert.Equal(t, 0, resp.Usage.CacheCreationTokens)
	assert.Equal(t, 2, resp.Usage.TotalTokens)
}

// TestOpenAICompat_Usage_ZeroCachedTokens_LegacyBehavior proves the cold-cache
// turn (details present, cached_tokens == 0) behaves identically to the
// no-details case — no cache-read split, whole prompt as input.
func TestOpenAICompat_Usage_ZeroCachedTokens_LegacyBehavior(t *testing.T) {
	srv := openAIFixedResponseServer(t, chatCompletionZeroCachedUsageResponse)
	p := newOpenRouterWithBase(t, srv.URL)

	resp, err := p.Chat(context.Background(), llm.ChatRequest{
		Model:    "gpt-5-mini",
		Messages: []llm.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)

	assert.Equal(t, 1000, resp.Usage.InputTokens)
	assert.Equal(t, 0, resp.Usage.CacheReadTokens)
}

// TestOpenAICompat_Usage_CachedTokensClampedToPrompt guards the malformed
// upstream where cached_tokens exceeds prompt_tokens: InputTokens must clamp at
// zero rather than going negative.
func TestOpenAICompat_Usage_CachedTokensClampedToPrompt(t *testing.T) {
	srv := openAIFixedResponseServer(t, chatCompletionOvercachedUsageResponse)
	p := newOpenRouterWithBase(t, srv.URL)

	resp, err := p.Chat(context.Background(), llm.ChatRequest{
		Model:    "gpt-5-mini",
		Messages: []llm.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)

	assert.Equal(t, 0, resp.Usage.InputTokens, "InputTokens must clamp at zero, never negative")
	assert.Equal(t, 1000, resp.Usage.CacheReadTokens, "cached is clamped to the reported prompt_tokens")
}
