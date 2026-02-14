package providers_test

import (
	"testing"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/llm/providers"
)

// Compile-time interface compliance checks
var (
	_ llm.Provider = (*providers.OpenRouterProvider)(nil)
	_ llm.Provider = (*providers.OpenAIProvider)(nil)
	_ llm.Provider = (*providers.AnthropicProvider)(nil)
)

func TestAllProviders_ImplementInterface(t *testing.T) {
	t.Run("openrouter", func(t *testing.T) {
		var _ llm.Provider = (*providers.OpenRouterProvider)(nil)
	})
	t.Run("openai", func(t *testing.T) {
		var _ llm.Provider = (*providers.OpenAIProvider)(nil)
	})
	t.Run("anthropic", func(t *testing.T) {
		var _ llm.Provider = (*providers.AnthropicProvider)(nil)
	})
}
