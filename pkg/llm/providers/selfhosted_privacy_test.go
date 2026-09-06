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

// TestSelfHostedProvider_DisablesProviderSideLogging pins the 152-ФЗ data
// minimisation contract: every request to a self-hosted (Yandex AI Studio)
// endpoint carries x-data-logging-enabled: false so the provider does not
// retain the prompt payload.
func TestSelfHostedProvider_DisablesProviderSideLogging(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("x-data-logging-enabled"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":     "chatcmpl-privacy",
			"object": "chat.completion",
			"choices": []map[string]interface{}{
				{
					"index":         0,
					"message":       map[string]interface{}{"role": "assistant", "content": "ok"},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]interface{}{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	}))
	defer srv.Close()

	p := providers.NewSelfHosted("selfhosted-0", srv.URL+"/v1", "")
	require.NotNil(t, p)

	_, err := p.Chat(context.Background(), llm.ChatRequest{
		UserID:    uuid.New(),
		Model:     "deepseek-v4-flash",
		Messages:  []llm.Message{{Role: "user", Content: "hi"}},
		MaxTokens: 5,
	})
	require.NoError(t, err)

	require.Len(t, seen, 1)
	assert.Equal(t, "false", seen[0], "self-hosted requests must opt out of provider-side logging")
}
