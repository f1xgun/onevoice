package providers_test

import (
	"context"
	"os"
	"testing"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/llm/providers"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnthropicProvider_Name(t *testing.T) {
	p := providers.NewAnthropic("test-key")
	assert.Equal(t, "anthropic", p.Name())
}

func TestAnthropicProvider_NilOnEmptyKey(t *testing.T) {
	p := providers.NewAnthropic("")
	assert.Nil(t, p)
}

func TestAnthropicProvider_Chat_Live(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	p := providers.NewAnthropic(apiKey)
	require.NotNil(t, p)

	resp, err := p.Chat(context.Background(), llm.ChatRequest{
		UserID:    uuid.New(),
		Model:     "claude-3-haiku-20240307",
		Messages:  []llm.Message{{Role: "user", Content: "Say hi"}},
		MaxTokens: 10,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Content)
	assert.Equal(t, "anthropic", resp.Provider)
}
