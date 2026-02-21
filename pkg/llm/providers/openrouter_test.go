package providers_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/llm/providers"
)

func TestOpenRouterProvider_Name(t *testing.T) {
	p := providers.NewOpenRouter("test-key")
	assert.Equal(t, "openrouter", p.Name())
}

func TestOpenRouterProvider_NilOnEmptyKey(t *testing.T) {
	p := providers.NewOpenRouter("")
	assert.Nil(t, p)
}

func TestOpenRouterProvider_Chat_Live(t *testing.T) {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		t.Skip("OPENROUTER_API_KEY not set")
	}

	p := providers.NewOpenRouter(apiKey)
	require.NotNil(t, p)

	resp, err := p.Chat(context.Background(), llm.ChatRequest{
		UserID:    uuid.New(),
		Model:     "openai/gpt-3.5-turbo",
		Messages:  []llm.Message{{Role: "user", Content: "Say hi"}},
		MaxTokens: 10,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Content)
	assert.Greater(t, resp.Usage.TotalTokens, 0)
	assert.Equal(t, "openrouter", resp.Provider)
}

func TestOpenRouterProvider_ListModels_Live(t *testing.T) {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		t.Skip("OPENROUTER_API_KEY not set")
	}

	p := providers.NewOpenRouter(apiKey)
	models, err := p.ListModels(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, models)
}
