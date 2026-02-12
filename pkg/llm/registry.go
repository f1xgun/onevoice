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
