package agentbase

import "os"

// GetEnv returns the value of the named environment variable, or
// defaultValue if the variable is unset or empty. Matches the per-agent
// getEnv helper that was duplicated across services/agent-*/cmd/main.go.
//
// An empty string and an unset variable are treated identically — call sites
// that need to distinguish the two should use os.LookupEnv directly.
func GetEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
