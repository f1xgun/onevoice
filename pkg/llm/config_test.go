package llm_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"

	"github.com/f1xgun/onevoice/pkg/llm"
)

func TestConfigUnmarshal(t *testing.T) {
	yamlData := `
providers:
  openrouter:
    enabled: true
    api_key_env: OPENROUTER_API_KEY
    priority: 1

commission:
  mode: tiered
  tiered:
    free: 30.0
    basic: 20.0
    pro: 10.0

model_filter:
  mode: whitelist
  whitelist:
    - claude-3.5-sonnet
    - gpt-4-turbo

pricing_overrides:
  claude-3.5-sonnet:
    input_per_1m: 3.00
    output_per_1m: 15.00

default_pricing:
  input_per_1m: 5.00
  output_per_1m: 15.00
`

	var cfg llm.Config
	err := yaml.Unmarshal([]byte(yamlData), &cfg)
	assert.NoError(t, err)

	assert.True(t, cfg.Providers["openrouter"].Enabled)
	assert.Equal(t, "OPENROUTER_API_KEY", cfg.Providers["openrouter"].APIKeyEnv)
	assert.Equal(t, 1, cfg.Providers["openrouter"].Priority)

	assert.Equal(t, "tiered", cfg.Commission.Mode)
	assert.Equal(t, 30.0, cfg.Commission.Tiered["free"])

	assert.Equal(t, "whitelist", cfg.ModelFilter.Mode)
	assert.Contains(t, cfg.ModelFilter.Whitelist, "claude-3.5-sonnet")

	assert.Equal(t, 3.0, cfg.PricingOverrides["claude-3.5-sonnet"].InputPer1M)
	assert.Equal(t, 5.0, cfg.DefaultPricing.InputPer1M)
}
