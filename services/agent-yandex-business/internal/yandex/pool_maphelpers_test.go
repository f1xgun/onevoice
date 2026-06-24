package yandex

// Test-only helpers that mirror the old sync.Map surface over the plain
// commitMu-guarded contexts map so existing tests can seed and inspect the pool
// without each one re-deriving the locking. Production code uses the typed
// internal helpers (cacheHit / buildAndCommit / *Locked) instead.

type testMap struct{ p *BrowserPool }

// contextsTest exposes the seed/inspect surface used by tests.
func (p *BrowserPool) contextsTest() testMap { return testMap{p} }

func (m testMap) Store(key string, pc *pooledContext) {
	m.p.commitMu.Lock()
	defer m.p.commitMu.Unlock()
	if m.p.contexts == nil {
		m.p.contexts = make(map[string]*pooledContext)
	}
	m.p.contexts[key] = pc
}

func (m testMap) Load(key string) (*pooledContext, bool) {
	m.p.commitMu.Lock()
	defer m.p.commitMu.Unlock()
	pc, ok := m.p.contexts[key]
	return pc, ok
}

func (m testMap) Delete(key string) {
	m.p.commitMu.Lock()
	defer m.p.commitMu.Unlock()
	delete(m.p.contexts, key)
}

func (m testMap) Range(fn func(key string, pc *pooledContext) bool) {
	m.p.commitMu.Lock()
	snapshot := make(map[string]*pooledContext, len(m.p.contexts))
	for k, v := range m.p.contexts {
		snapshot[k] = v
	}
	m.p.commitMu.Unlock()
	for k, v := range snapshot {
		if !fn(k, v) {
			return
		}
	}
}
