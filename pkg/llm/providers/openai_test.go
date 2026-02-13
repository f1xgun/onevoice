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

func TestOpenAIProvider_Name(t *testing.T) {
	p := providers.NewOpenAI("test-key")
	assert.Equal(t, "openai", p.Name())
}

func TestOpenAIProvider_NilOnEmptyKey(t *testing.T) {
	p := providers.NewOpenAI("")
	assert.Nil(t, p)
}

func TestOpenAIProvider_Chat_Live(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	p := providers.NewOpenAI(apiKey)
	require.NotNil(t, p)

	resp, err := p.Chat(context.Background(), llm.ChatRequest{
		UserID:    uuid.New(),
		Model:     "gpt-3.5-turbo",
		Messages:  []llm.Message{{Role: "user", Content: "Say hi"}},
		MaxTokens: 10,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Content)
	assert.Equal(t, "openai", resp.Provider)
}
