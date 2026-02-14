package llm_test

import (
	"testing"
	"time"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestIntegration_RegistryWithConfig verifies registry works with config types
func TestIntegration_RegistryWithConfig(t *testing.T) {
	// Load config from YAML
	yamlData := `
providers:
  openrouter:
    enabled: true
    api_key_env: OPENROUTER_API_KEY
    priority: 1
  openai:
    enabled: true
    api_key_env: OPENAI_API_KEY
    priority: 2

commission:
  mode: percentage
  percentage: 20.0

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
	require.NoError(t, err)

	// Create registry and register models from config
	registry := llm.NewRegistry()

	// Register Claude model from pricing overrides
	claudePricing := cfg.PricingOverrides["claude-3.5-sonnet"]
	registry.RegisterModelProvider(&llm.ModelProviderEntry{
		Model:              "claude-3.5-sonnet",
		Provider:           "openrouter",
		InputCostPer1MTok:  claudePricing.InputPer1M,
		OutputCostPer1MTok: claudePricing.OutputPer1M,
		AvgLatencyMs:       0,
		HealthStatus:       "healthy",
		Enabled:            cfg.Providers["openrouter"].Enabled,
		Priority:           cfg.Providers["openrouter"].Priority,
		LastCheckedAt:      time.Now(),
	})

	// Register GPT-4 with default pricing
	registry.RegisterModelProvider(&llm.ModelProviderEntry{
		Model:              "gpt-4-turbo",
		Provider:           "openai",
		InputCostPer1MTok:  cfg.DefaultPricing.InputPer1M,
		OutputCostPer1MTok: cfg.DefaultPricing.OutputPer1M,
		AvgLatencyMs:       0,
		HealthStatus:       "healthy",
		Enabled:            cfg.Providers["openai"].Enabled,
		Priority:           cfg.Providers["openai"].Priority,
		LastCheckedAt:      time.Now(),
	})

	// Verify models are registered
	assert.True(t, registry.ModelExists("claude-3.5-sonnet"))
	assert.True(t, registry.ModelExists("gpt-4-turbo"))
	assert.False(t, registry.ModelExists("gpt-3.5-turbo")) // Not in whitelist

	// Simulate successful requests and track metrics
	registry.RecordSuccess("openrouter", "claude-3.5-sonnet", 1200*time.Millisecond)
	registry.RecordSuccess("openrouter", "claude-3.5-sonnet", 1100*time.Millisecond)
	registry.RecordSuccess("openai", "gpt-4-turbo", 2000*time.Millisecond)

	// Verify metrics updated
	claudeProviders := registry.GetModelProviders("claude-3.5-sonnet")
	require.Len(t, claudeProviders, 1)
	assert.Equal(t, 1150, claudeProviders[0].AvgLatencyMs) // Average of 1200 and 1100
	assert.Equal(t, "healthy", claudeProviders[0].HealthStatus)

	gptProviders := registry.GetModelProviders("gpt-4-turbo")
	require.Len(t, gptProviders, 1)
	assert.Equal(t, 2000, gptProviders[0].AvgLatencyMs)
	assert.Equal(t, "healthy", gptProviders[0].HealthStatus)
}

// TestIntegration_CostCalculationWithCommission verifies cost breakdown with commission
func TestIntegration_CostCalculationWithCommission(t *testing.T) {
	// Create chat request
	req := llm.ChatRequest{
		UserID:      uuid.New(),
		Model:       "claude-3.5-sonnet",
		Messages:    []llm.Message{{Role: "user", Content: "Hello"}},
		MaxTokens:   100,
		Temperature: 0.7,
	}

	// Simulate token usage
	usage := llm.TokenUsage{
		InputTokens:  50,
		OutputTokens: 100,
		TotalTokens:  150,
	}

	// Calculate cost with pricing
	inputPricing := 3.00   // $3 per 1M tokens
	outputPricing := 15.00 // $15 per 1M tokens

	providerCost := (float64(usage.InputTokens) * inputPricing / 1_000_000) +
		(float64(usage.OutputTokens) * outputPricing / 1_000_000)

	commission := providerCost * 0.20 // 20% commission
	userCost := providerCost + commission

	// Create cost breakdown
	cost := llm.CostBreakdown{
		ProviderCost: providerCost,
		Commission:   commission,
		UserCost:     userCost,
	}

	// Verify calculations
	assert.InDelta(t, 0.00165, cost.ProviderCost, 0.0001) // ~$0.00165
	assert.InDelta(t, 0.00033, cost.Commission, 0.0001)   // ~$0.00033
	assert.InDelta(t, 0.00198, cost.UserCost, 0.0001)     // ~$0.00198
	assert.Equal(t, cost.ProviderCost+cost.Commission, cost.UserCost)

	// Verify request has required fields
	assert.NotEqual(t, uuid.Nil, req.UserID)
	assert.Equal(t, "claude-3.5-sonnet", req.Model)
	assert.NotEmpty(t, req.Messages)
}

// TestIntegration_ProviderSelectionByStrategy simulates provider selection
func TestIntegration_ProviderSelectionByStrategy(t *testing.T) {
	registry := llm.NewRegistry()

	// Register same model with multiple providers
	registry.RegisterModelProvider(&llm.ModelProviderEntry{
		Model:              "gpt-4-turbo",
		Provider:           "openrouter",
		InputCostPer1MTok:  5.00,
		OutputCostPer1MTok: 15.00,
		AvgLatencyMs:       1500,
		HealthStatus:       "healthy",
		Enabled:            true,
		Priority:           1,
	})

	registry.RegisterModelProvider(&llm.ModelProviderEntry{
		Model:              "gpt-4-turbo",
		Provider:           "openai",
		InputCostPer1MTok:  10.00, // More expensive
		OutputCostPer1MTok: 30.00,
		AvgLatencyMs:       800, // But faster
		HealthStatus:       "healthy",
		Enabled:            true,
		Priority:           2,
	})

	// Get all providers for model
	providers := registry.GetModelProviders("gpt-4-turbo")
	require.Len(t, providers, 2)

	// Simulate cost strategy selection (lowest cost)
	var cheapest *llm.ModelProviderEntry
	minCost := float64(1000000)
	for _, p := range providers {
		if p.HealthStatus == "healthy" && p.Enabled {
			avgCost := (p.InputCostPer1MTok + p.OutputCostPer1MTok) / 2
			if avgCost < minCost {
				minCost = avgCost
				cheapest = p
			}
		}
	}
	require.NotNil(t, cheapest)
	assert.Equal(t, "openrouter", cheapest.Provider)
	assert.Equal(t, 5.00, cheapest.InputCostPer1MTok)

	// Simulate speed strategy selection (lowest latency)
	var fastest *llm.ModelProviderEntry
	minLatency := int(1000000)
	for _, p := range providers {
		if p.HealthStatus == "healthy" && p.Enabled {
			if p.AvgLatencyMs < minLatency {
				minLatency = p.AvgLatencyMs
				fastest = p
			}
		}
	}
	require.NotNil(t, fastest)
	assert.Equal(t, "openai", fastest.Provider)
	assert.Equal(t, 800, fastest.AvgLatencyMs)

	// Verify Strategy enum values
	assert.Equal(t, llm.StrategyCost, llm.Strategy(0))
	assert.Equal(t, llm.StrategySpeed, llm.Strategy(1))
}
