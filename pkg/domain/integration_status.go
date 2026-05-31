package domain

// Integration.Status values. Persisted in integrations.status (text column,
// no DB-side enum) and read by every service-layer check that decides whether
// a connection is usable. Centralized here so a future rename ("active" →
// "connected") is a single edit rather than a grep-and-replace.
const (
	// IntegrationStatusActive — token is valid and the agent should accept
	// dispatches for this integration. Set on successful OAuth / paste-token
	// flows and after a successful refresh.
	IntegrationStatusActive = "active"
)
