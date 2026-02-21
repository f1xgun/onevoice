package providers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/llm/providers"
)

func TestSelfHostedProvider_Name(t *testing.T) {
	p := providers.NewSelfHosted("selfhosted-0", "http://localhost:11434/v1", "")
	require.NotNil(t, p)
	assert.Equal(t, "selfhosted-0", p.Name())
}

func TestSelfHostedProvider_NilOnEmptyName(t *testing.T) {
	p := providers.NewSelfHosted("", "http://localhost:11434/v1", "")
	assert.Nil(t, p)
}

func TestSelfHostedProvider_NilOnEmptyURL(t *testing.T) {
	p := providers.NewSelfHosted("selfhosted-0", "", "")
	assert.Nil(t, p)
}

func TestSelfHostedProvider_Chat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":     "chatcmpl-test",
			"object": "chat.completion",
			"choices": []map[string]interface{}{
				{
					"index":         0,
					"message":       map[string]interface{}{"role": "assistant", "content": "hello"},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]interface{}{
				"prompt_tokens":     5,
				"completion_tokens": 3,
				"total_tokens":      8,
			},
		})
	}))
	defer srv.Close()

	p := providers.NewSelfHosted("selfhosted-0", srv.URL+"/v1", "")
	require.NotNil(t, p)
	resp, err := p.Chat(context.Background(), llm.ChatRequest{
		UserID:    uuid.New(),
		Model:     "llama3.1",
		Messages:  []llm.Message{{Role: "user", Content: "hi"}},
		MaxTokens: 10,
	})
	require.NoError(t, err)
	assert.Equal(t, "hello", resp.Content)
	assert.Equal(t, "selfhosted-0", resp.Provider)
	assert.Equal(t, 8, resp.Usage.TotalTokens)
}
