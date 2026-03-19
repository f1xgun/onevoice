package health

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// CheckFunc checks a single dependency and returns nil if healthy.
type CheckFunc func(ctx context.Context) error

// Checker holds named health checks.
type Checker struct {
	mu     sync.RWMutex
	checks map[string]CheckFunc
}

// New creates a Checker with no checks registered.
func New() *Checker {
	return &Checker{checks: make(map[string]CheckFunc)}
}

// AddCheck registers a named health check.
func (c *Checker) AddCheck(name string, fn CheckFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.checks[name] = fn
}

// LiveHandler returns 200 {"status":"alive"} always.
func (c *Checker) LiveHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"alive"}`))
	}
}

// ReadyHandler runs all checks with a 5-second timeout.
// Returns 200 {"status":"ready","checks":{...}} if all pass,
// or 503 {"status":"not_ready","checks":{...}} if any fail.
func (c *Checker) ReadyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		c.mu.RLock()
		checks := make(map[string]CheckFunc, len(c.checks))
		for k, v := range c.checks {
			checks[k] = v
		}
		c.mu.RUnlock()

		results := make(map[string]string, len(checks))
		allOK := true
		for name, fn := range checks {
			if err := fn(ctx); err != nil {
				results[name] = err.Error()
				allOK = false
			} else {
				results[name] = "ok"
			}
		}

		status := "ready"
		httpStatus := http.StatusOK
		if !allOK {
			status = "not_ready"
			httpStatus = http.StatusServiceUnavailable
		}

		resp := map[string]interface{}{
			"status": status,
			"checks": results,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(httpStatus)
		_ = json.NewEncoder(w).Encode(resp)
	}
}
