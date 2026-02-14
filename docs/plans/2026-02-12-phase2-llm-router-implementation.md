# Phase 2.1: LLM Router & Provider Abstraction - Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Build LLM router that automatically selects optimal provider (OpenRouter/OpenAI/Anthropic) with smart routing, rate limiting, billing, and commission tracking.

**Architecture:** Abstract provider interface → Provider adapters (OpenRouter, OpenAI, Anthropic) → Dynamic registry with pricing → Router with strategy-based selection → Rate limiter (Redis) → Billing with commission.

**Tech Stack:** Go 1.24, go-openai, anthropic-sdk-go, Redis, PostgreSQL, YAML config

**Context:** This is Phase 2.1 of the full Phase 2 LLM Orchestrator. Builds foundational LLM routing layer that Phase 2.2 (Orchestrator Core) will use.

---

## Task 1: Provider Interface & Base Types

**Files:**
- Create: `pkg/llm/provider.go`
- Create: `pkg/llm/types.go`

**Step 1: Write the failing test**

Create `pkg/llm/provider_test.go`:
```go
package llm_test

import (
	"context"
	"testing"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/stretchr/testify/assert"
)

func TestProviderInterface(t *testing.T) {
	// Test that provider interface can be implemented
	var _ llm.Provider = (*mockProvider)(nil)
}

type mockProvider struct{}

func (m *mockProvider) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{
		Content: "test",
		Usage:   llm.TokenUsage{InputTokens: 10, OutputTokens: 20, TotalTokens: 30},
	}, nil
}

func (m *mockProvider) ChatStream(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}

func (m *mockProvider) ListModels(ctx context.Context) ([]llm.ModelInfo, error) {
	return []llm.ModelInfo{{ID: "test-model", Provider: "test"}}, nil
}

func (m *mockProvider) HealthCheck(ctx context.Context) error {
	return nil
}

func (m *mockProvider) Name() string {
	return "mock"
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/llm/... -v`
Expected: FAIL - package llm does not exist

**Step 3: Write minimal implementation**

Create `pkg/llm/types.go`:
```go
package llm

import (
	"time"

	"github.com/google/uuid"
)

// Message represents a chat message
type Message struct {
	Role       string     `json:"role"` // "system", "user", "assistant", "tool"
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall represents an LLM function call
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"` // "function"
	Function FunctionCall `json:"function"`
}

// FunctionCall contains function name and arguments
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}

// ToolDefinition defines an available tool
type ToolDefinition struct {
	Type     string             `json:"type"` // "function"
	Function FunctionDefinition `json:"function"`
}

// FunctionDefinition describes a function's schema
type FunctionDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"` // JSON Schema
}

// ChatRequest is normalized request format
type ChatRequest struct {
	UserID      uuid.UUID
	Model       string
	Messages    []Message
	Tools       []ToolDefinition
	MaxTokens   int
	Temperature float64
	TopP        float64
	Stop        []string

	// Metadata
	RequestID string
}

// ChatResponse is normalized response format
type ChatResponse struct {
	Content      string
	ToolCalls    []ToolCall
	Usage        TokenUsage
	FinishReason string // "stop", "length", "tool_calls"
	Latency      time.Duration
	RawResponse  interface{}
}

// TokenUsage tracks token consumption
type TokenUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// StreamChunk represents incremental streaming response
type StreamChunk struct {
	Delta         string
	ToolCallDelta *ToolCall
	Usage         *TokenUsage
	Done          bool
	Error         error
}

// ModelInfo describes a model from provider API
type ModelInfo struct {
	ID                 string
	Name               string
	Provider           string
	InputCostPer1MTok  *float64
	OutputCostPer1MTok *float64
	ContextLength      int
	SupportsToolUse    bool
	SupportsStreaming  bool
	SupportsVision     bool
}

// Strategy defines routing strategy
type Strategy int

const (
	StrategyCost Strategy = iota // Minimize cost (default)
	StrategySpeed                 // Minimize latency
)
```

Create `pkg/llm/provider.go`:
```go
package llm

import "context"

// Provider is the unified interface all LLM providers must implement
type Provider interface {
	// Chat performs synchronous chat completion
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)

	// ChatStream performs streaming chat completion
	ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error)

	// ListModels returns available models with pricing
	ListModels(ctx context.Context) ([]ModelInfo, error)

	// HealthCheck verifies provider availability
	HealthCheck(ctx context.Context) error

	// Name returns provider identifier
	Name() string
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/llm/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/llm/
git commit -m "feat(llm): add provider interface and base types

- Define Provider interface with Chat, Stream, ListModels, Health
- Add normalized types: ChatRequest, ChatResponse, Message, ToolCall
- Add ModelInfo for dynamic model discovery
- Add Strategy enum for cost/speed routing"
```

---

## Task 2: Cost Breakdown & Commission Types

**Files:**
- Modify: `pkg/llm/types.go`

**Step 1: Write the failing test**

Add to `pkg/llm/provider_test.go`:
```go
func TestCostBreakdown(t *testing.T) {
	cost := llm.CostBreakdown{
		ProviderCost: 0.045,
		Commission:   0.009,
		UserCost:     0.054,
	}

	assert.Equal(t, 0.045, cost.ProviderCost)
	assert.Equal(t, 0.009, cost.Commission)
	assert.Equal(t, 0.054, cost.UserCost)
	assert.Equal(t, cost.ProviderCost+cost.Commission, cost.UserCost)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/llm/... -v`
Expected: FAIL - undefined: llm.CostBreakdown

**Step 3: Write minimal implementation**

Add to `pkg/llm/types.go`:
```go
// CostBreakdown separates provider cost from platform commission
type CostBreakdown struct {
	ProviderCost float64 `json:"provider_cost"` // Actual cost to LLM provider
	Commission   float64 `json:"commission"`    // OneVoice markup
	UserCost     float64 `json:"user_cost"`     // Total charged to user
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/llm/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/llm/types.go pkg/llm/provider_test.go
git commit -m "feat(llm): add cost breakdown with commission tracking"
```

---

## Task 3: Configuration Types

**Files:**
- Create: `pkg/llm/config.go`

**Step 1: Write the failing test**

Create `pkg/llm/config_test.go`:
```go
package llm_test

import (
	"testing"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/llm/... -v`
Expected: FAIL - undefined: llm.Config

**Step 3: Write minimal implementation**

Create `pkg/llm/config.go`:
```go
package llm

// Config represents llm.yaml configuration
type Config struct {
	Providers        map[string]ProviderConfig `yaml:"providers"`
	Commission       CommissionConfig          `yaml:"commission"`
	ModelFilter      ModelFilterConfig         `yaml:"model_filter"`
	PricingOverrides map[string]PricingInfo    `yaml:"pricing_overrides"`
	DefaultPricing   PricingInfo               `yaml:"default_pricing"`
}

// ProviderConfig configures a single provider
type ProviderConfig struct {
	Enabled   bool   `yaml:"enabled"`
	APIKeyEnv string `yaml:"api_key_env"`
	Priority  int    `yaml:"priority"`
}

// CommissionConfig defines platform markup strategy
type CommissionConfig struct {
	Mode       string             `yaml:"mode"` // "percentage", "flat", "tiered"
	Percentage float64            `yaml:"percentage"`
	FlatFeeUSD float64            `yaml:"flat_fee_usd"`
	Tiered     map[string]float64 `yaml:"tiered"` // tier -> percentage
}

// ModelFilterConfig controls which models to enable
type ModelFilterConfig struct {
	Mode      string   `yaml:"mode"` // "whitelist", "blacklist", "all"
	Whitelist []string `yaml:"whitelist"`
	Blacklist []string `yaml:"blacklist"`
}

// PricingInfo defines model pricing
type PricingInfo struct {
	InputPer1M  float64 `yaml:"input_per_1m"`
	OutputPer1M float64 `yaml:"output_per_1m"`
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/llm/... -v`
Expected: PASS

**Step 5: Add yaml dependency and commit**

```bash
cd pkg && go get gopkg.in/yaml.v3
git add pkg/llm/config.go pkg/llm/config_test.go pkg/go.mod pkg/go.sum
git commit -m "feat(llm): add configuration types for llm.yaml

- Define Config struct for provider, commission, filter settings
- Support multiple commission modes (percentage/flat/tiered)
- Add model whitelist/blacklist filtering
- Add pricing overrides per model"
```

---

## Task 4: Model Registry - Data Structures

**Files:**
- Create: `pkg/llm/registry.go`

**Step 1: Write the failing test**

Create `pkg/llm/registry_test.go`:
```go
package llm_test

import (
	"testing"
	"time"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/stretchr/testify/assert"
)

func TestRegistry_RegisterModelProvider(t *testing.T) {
	registry := llm.NewRegistry()

	entry := &llm.ModelProviderEntry{
		Model:              "claude-3.5-sonnet",
		Provider:           "openrouter",
		InputCostPer1MTok:  3.00,
		OutputCostPer1MTok: 15.00,
		AvgLatencyMs:       1200,
		HealthStatus:       "healthy",
		Enabled:            true,
		Priority:           1,
		LastCheckedAt:      time.Now(),
	}

	registry.RegisterModelProvider(entry)

	providers := registry.GetModelProviders("claude-3.5-sonnet")
	assert.Len(t, providers, 1)
	assert.Equal(t, "openrouter", providers[0].Provider)
	assert.Equal(t, 3.0, providers[0].InputCostPer1MTok)
}

func TestRegistry_ModelExists(t *testing.T) {
	registry := llm.NewRegistry()

	assert.False(t, registry.ModelExists("nonexistent"))

	registry.RegisterModelProvider(&llm.ModelProviderEntry{
		Model:    "gpt-4",
		Provider: "openai",
	})

	assert.True(t, registry.ModelExists("gpt-4"))
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/llm/... -v`
Expected: FAIL - undefined: llm.NewRegistry

**Step 3: Write minimal implementation**

Create `pkg/llm/registry.go`:
```go
package llm

import (
	"sync"
	"time"
)

// ModelProviderEntry represents a model-provider pair with metadata
type ModelProviderEntry struct {
	Model              string
	Provider           string
	InputCostPer1MTok  float64
	OutputCostPer1MTok float64
	AvgLatencyMs       int
	HealthStatus       string // "healthy", "degraded", "down"
	Enabled            bool
	Priority           int
	LastCheckedAt      time.Time
}

// ProviderMetrics tracks provider performance
type ProviderMetrics struct {
	TotalRequests   int64
	SuccessCount    int64
	FailureCount    int64
	AvgLatencyMs    int
	LastLatencies   []int64 // Rolling window of last 100
	LastHealthCheck time.Time
	HealthStatus    string
}

// Registry maintains model-provider mappings
type Registry struct {
	mu      sync.RWMutex
	entries map[string][]*ModelProviderEntry // Key: model name
	metrics map[string]*ProviderMetrics      // Key: "provider:model"
}

// NewRegistry creates a new registry
func NewRegistry() *Registry {
	return &Registry{
		entries: make(map[string][]*ModelProviderEntry),
		metrics: make(map[string]*ProviderMetrics),
	}
}

// RegisterModelProvider adds or updates a model-provider pair
func (r *Registry) RegisterModelProvider(entry *ModelProviderEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entries := r.entries[entry.Model]

	// Check if already exists (update)
	for i, e := range entries {
		if e.Provider == entry.Provider {
			entries[i] = entry
			return
		}
	}

	// Add new
	r.entries[entry.Model] = append(entries, entry)

	// Initialize metrics
	key := entry.Provider + ":" + entry.Model
	if _, exists := r.metrics[key]; !exists {
		r.metrics[key] = &ProviderMetrics{
			HealthStatus:  "healthy",
			LastLatencies: make([]int64, 0, 100),
		}
	}
}

// GetModelProviders returns all providers supporting a model
func (r *Registry) GetModelProviders(model string) []*ModelProviderEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entries := r.entries[model]
	result := make([]*ModelProviderEntry, len(entries))
	copy(result, entries)

	return result
}

// ModelExists checks if model is registered
func (r *Registry) ModelExists(model string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.entries[model]) > 0
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/llm/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/llm/registry.go pkg/llm/registry_test.go
git commit -m "feat(llm): add model registry with provider tracking

- Implement thread-safe registry for model-provider pairs
- Track pricing, latency, health status per pair
- Support registration, lookup, existence checks
- Initialize provider metrics on registration"
```

---

## Task 5: Registry - Metrics Recording

**Files:**
- Modify: `pkg/llm/registry.go`
- Modify: `pkg/llm/registry_test.go`

**Step 1: Write the failing test**

Add to `pkg/llm/registry_test.go`:
```go
func TestRegistry_RecordSuccess(t *testing.T) {
	registry := llm.NewRegistry()

	entry := &llm.ModelProviderEntry{
		Model:        "test-model",
		Provider:     "test-provider",
		AvgLatencyMs: 0,
	}
	registry.RegisterModelProvider(entry)

	// Record success with 1000ms latency
	registry.RecordSuccess("test-provider", "test-model", 1000*time.Millisecond)

	// Verify metrics updated
	providers := registry.GetModelProviders("test-model")
	assert.Equal(t, 1000, providers[0].AvgLatencyMs)
	assert.Equal(t, "healthy", providers[0].HealthStatus)
}

func TestRegistry_RecordFailure(t *testing.T) {
	registry := llm.NewRegistry()

	entry := &llm.ModelProviderEntry{
		Model:        "test-model",
		Provider:     "test-provider",
		HealthStatus: "healthy",
	}
	registry.RegisterModelProvider(entry)

	// Record 6 failures (>50% failure rate)
	for i := 0; i < 6; i++ {
		registry.RecordFailure("test-provider", "test-model")
	}

	// Verify health status degraded
	providers := registry.GetModelProviders("test-model")
	assert.Equal(t, "down", providers[0].HealthStatus)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/llm/... -v -run TestRegistry_Record`
Expected: FAIL - undefined: Registry.RecordSuccess

**Step 3: Write minimal implementation**

Add to `pkg/llm/registry.go`:
```go
// RecordSuccess updates metrics after successful request
func (r *Registry) RecordSuccess(provider, model string, latency time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := provider + ":" + model
	metrics := r.metrics[key]
	if metrics == nil {
		return
	}

	metrics.TotalRequests++
	metrics.SuccessCount++

	// Update latency rolling window
	latencyMs := latency.Milliseconds()
	metrics.LastLatencies = append(metrics.LastLatencies, latencyMs)
	if len(metrics.LastLatencies) > 100 {
		metrics.LastLatencies = metrics.LastLatencies[1:]
	}

	// Recalculate average
	var sum int64
	for _, l := range metrics.LastLatencies {
		sum += l
	}
	metrics.AvgLatencyMs = int(sum / int64(len(metrics.LastLatencies)))

	// Update health status
	if metrics.HealthStatus == "down" || metrics.HealthStatus == "degraded" {
		if metrics.SuccessCount >= 3 {
			metrics.HealthStatus = "healthy"
		}
	}

	// Update entry in registry
	for _, entry := range r.entries[model] {
		if entry.Provider == provider {
			entry.AvgLatencyMs = metrics.AvgLatencyMs
			entry.HealthStatus = metrics.HealthStatus
			entry.LastCheckedAt = time.Now()
			break
		}
	}
}

// RecordFailure updates metrics after failed request
func (r *Registry) RecordFailure(provider, model string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := provider + ":" + model
	metrics := r.metrics[key]
	if metrics == nil {
		return
	}

	metrics.TotalRequests++
	metrics.FailureCount++

	// Calculate failure rate
	failureRate := float64(metrics.FailureCount) / float64(metrics.TotalRequests)

	// Update health status based on failure rate
	var newStatus string
	if failureRate > 0.5 {
		newStatus = "down"
	} else if failureRate > 0.2 {
		newStatus = "degraded"
	} else {
		newStatus = "healthy"
	}

	metrics.HealthStatus = newStatus

	// Update entry in registry
	for _, entry := range r.entries[model] {
		if entry.Provider == provider {
			entry.HealthStatus = newStatus
			entry.LastCheckedAt = time.Now()
			break
		}
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/llm/... -v -run TestRegistry_Record`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/llm/registry.go pkg/llm/registry_test.go
git commit -m "feat(llm): add metrics recording to registry

- Record success with latency tracking (rolling 100-sample average)
- Record failures with health status degradation
- Update health: >50% fail=down, >20%=degraded, else=healthy
- Automatic recovery after 3 consecutive successes"
```

---

## Execution Handoff

Plan complete and saved to `docs/plans/2026-02-12-phase2-llm-router-implementation.md`.

**This plan covers Tasks 1-5 (foundational types and registry).** The full Phase 2.1 has 13 tasks total:
- Tasks 1-5: Types, config, registry (THIS PLAN)
- Tasks 6-8: Rate limiter, billing repository
- Tasks 9-11: OpenRouter, OpenAI, Anthropic providers
- Tasks 12-13: Router with smart selection

**Two execution options:**

**1. Subagent-Driven (this session)** - I dispatch fresh subagent per task, review between tasks, fast iteration

**2. Parallel Session (separate)** - Open new session with executing-plans, batch execution with checkpoints

**Which approach?**
