// Package health provides shared liveness / readiness HTTP handlers for
// every OneVoice service. ReadyHandler runs all registered dependency checks
// concurrently (sync.WaitGroup + per-check context.WithTimeout) so total
// wall-clock cost is bounded by the slowest single check rather than the
// sum of all checks — required by the k8s readinessProbe default of 5s.
//
// JSON contract:
//
//	200 OK   → {"status":"ready","checks":{"postgres":"ok",...}}
//	503      → {"status":"unhealthy","failed":["redis"],"checks":{"postgres":"ok","redis":"<err>",...}}
//
// Use New() for the default (2s per-check budget) or
// New(WithCheckTimeout(d)) to override for the calling service. The wiring
// helper RegisterDefaultChecks (in wiring.go) attaches the standard
// PG/Mongo/Redis/NATS checks against live clients.
package health

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"sync"
	"time"
)

// defaultCheckTimeout caps any single registered check. The per-check budget
// IS the per-handler budget because checks run concurrently (max, not sum).
// 2s gives ~20× headroom over a healthy <100ms response while staying well
// inside the k8s readinessProbe default (5s). Operators can override via
// WithCheckTimeout (typically wired through env HEALTH_CHECK_TIMEOUT).
const defaultCheckTimeout = 2 * time.Second

// CheckFunc checks a single dependency and returns nil if healthy.
type CheckFunc func(ctx context.Context) error

// Checker holds named health checks.
type Checker struct {
	mu           sync.RWMutex
	checks       map[string]CheckFunc
	checkTimeout time.Duration
}

// Option configures a Checker at construction time.
type Option func(*Checker)

// WithCheckTimeout overrides the default 2s per-check deadline. Values ≤0
// are ignored so an operator misconfiguration (zero / negative) can't disable
// the safety net — never panic on bad input.
func WithCheckTimeout(d time.Duration) Option {
	return func(c *Checker) {
		if d > 0 {
			c.checkTimeout = d
		}
	}
}

// New creates a Checker with no checks registered. Apply Option arguments to
// override defaults (currently only WithCheckTimeout).
func New(opts ...Option) *Checker {
	c := &Checker{
		checks:       make(map[string]CheckFunc),
		checkTimeout: defaultCheckTimeout,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
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

// readyResult is the in-flight result a single check goroutine sends to the
// fan-in channel.
type readyResult struct {
	name   string
	status string // "ok" or err.Error()
	ok     bool
}

// ReadyHandler runs every registered check concurrently. Each check gets its
// own context.WithTimeout(checkTimeout). Total wall-clock budget = checkTimeout
// (max not sum). Returns 200 + ready JSON when all checks pass; 503 +
// unhealthy JSON with a sorted `failed[]` slice on any failure.
func (c *Checker) ReadyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c.mu.RLock()
		snapshot := make(map[string]CheckFunc, len(c.checks))
		for k, v := range c.checks {
			snapshot[k] = v
		}
		timeout := c.checkTimeout
		c.mu.RUnlock()

		results := make(chan readyResult, len(snapshot))
		var wg sync.WaitGroup
		for name, fn := range snapshot {
			wg.Add(1)
			go func(name string, fn CheckFunc) {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(r.Context(), timeout)
				defer cancel()
				if err := fn(ctx); err != nil {
					results <- readyResult{name: name, status: err.Error(), ok: false}
					return
				}
				results <- readyResult{name: name, status: "ok", ok: true}
			}(name, fn)
		}
		wg.Wait()
		close(results)

		checks := make(map[string]string, len(snapshot))
		var failed []string
		for res := range results {
			checks[res.name] = res.status
			if !res.ok {
				failed = append(failed, res.name)
			}
		}
		sort.Strings(failed)

		w.Header().Set("Content-Type", "application/json")

		if len(failed) == 0 {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "ready",
				"checks": checks,
			})
			return
		}

		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "unhealthy",
			"failed": failed,
			"checks": checks,
		})
	}
}
