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
	switch {
	case failureRate > 0.5:
		newStatus = "down"
	case failureRate > 0.2:
		newStatus = "degraded"
	default:
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
