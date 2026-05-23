package llm

import (
	"sync"
	"time"
)

// Provider health states surfaced via ModelProviderEntry.HealthStatus.
// The transitions between these states live in pkg/llm/selector.go on
// defaultSelector — Registry only stores config.
const (
	HealthStatusHealthy  = "healthy"
	HealthStatusDegraded = "degraded"
	HealthStatusDown     = "down"
)

// ModelProviderEntry is the config record for a single model+provider
// pairing: pricing, priority, enabled flag, and the policy state that the
// Selector mirrors back onto it (HealthStatus, AvgLatencyMs,
// LastCheckedAt). The fields the Selector writes are mutated through the
// pointer Registry.GetModelProviders hands out so admin / status callers
// can read live state off the entry without going through the Selector.
type ModelProviderEntry struct {
	Model              string
	Provider           string
	InputCostPer1MTok  float64
	OutputCostPer1MTok float64
	AvgLatencyMs       int
	HealthStatus       string // one of HealthStatus* constants; mutated by Selector
	Enabled            bool
	Priority           int
	LastCheckedAt      time.Time
}

// Registry is the config store for model → providers. It no longer owns
// runtime metrics (rolling latency window, success/failure counts, health
// transitions) — those concerns moved to pkg/llm/selector.go where the
// pick algorithm that consumes them also lives. This keeps Registry as a
// pure data layer: callers register entries at boot, the Selector
// consults them per request.
type Registry struct {
	mu      sync.RWMutex
	entries map[string][]*ModelProviderEntry // key: model name
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		entries: make(map[string][]*ModelProviderEntry),
	}
}

// RegisterModelProvider adds or updates the entry for a (model, provider)
// pair. If an entry already exists for that pair the slot is overwritten
// in place — the same pointer is preserved so any Selector holding a
// reference to it (via a prior Pick) keeps seeing the new config.
func (r *Registry) RegisterModelProvider(entry *ModelProviderEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entries := r.entries[entry.Model]
	for i, e := range entries {
		if e.Provider == entry.Provider {
			entries[i] = entry
			return
		}
	}
	r.entries[entry.Model] = append(entries, entry)
}

// GetModelProviders returns a defensive copy of the slice of entries for
// `model`. The pointers inside are NOT copied — they point to the same
// entries the Selector mutates so callers reading HealthStatus etc. see
// live state.
func (r *Registry) GetModelProviders(model string) []*ModelProviderEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entries := r.entries[model]
	result := make([]*ModelProviderEntry, len(entries))
	copy(result, entries)
	return result
}

// ModelExists reports whether any provider is registered for `model`.
func (r *Registry) ModelExists(model string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.entries[model]) > 0
}
