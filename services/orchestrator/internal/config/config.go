package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds orchestrator configuration loaded from environment.
type Config struct {
	Port            string
	LLMModel        string
	LLMTier         string
	MaxIterations   int
	NATSUrl         string
	ShutdownTimeout time.Duration
	// ToolExecTimeout bounds a single tool call. Zero disables the per-tool
	// deadline — the request context still governs overall cancellation.
	ToolExecTimeout time.Duration

	// MongoDB connection (Phase 16 HITL — Plan 16-02 Task 2). The
	// orchestrator writes pending_tool_calls batches at pause time, so it
	// needs its own Mongo connection (avoids a circular dependency where
	// orchestrator → API → orchestrator). Defaults match the API service's
	// docker-compose values so dev setups that only set one MONGO_URI
	// continue to work.
	MongoURI string
	MongoDB  string

	// LLM provider API keys (at least one must be set)
	OpenRouterAPIKey string
	OpenAIAPIKey     string
	AnthropicAPIKey  string

	SelfHostedEndpoints []SelfHostedEndpoint
}

// SelfHostedEndpoint holds configuration for one self-hosted LLM inference endpoint.
type SelfHostedEndpoint struct {
	URL    string
	Model  string
	APIKey string // optional
}

// Load reads config from environment variables.
func Load() (*Config, error) {
	model := os.Getenv("LLM_MODEL")
	if model == "" {
		return nil, fmt.Errorf("LLM_MODEL is required")
	}

	maxIter := 10
	if v := os.Getenv("MAX_ITERATIONS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxIter = n
		}
	}

	shutdownTimeout := 30 * time.Second
	if v := os.Getenv("SHUTDOWN_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			shutdownTimeout = d
		}
	}

	var toolExecTimeout time.Duration
	if v := os.Getenv("TOOL_EXEC_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			toolExecTimeout = d
		}
	}

	return &Config{
		Port:            getEnv("PORT", "8090"),
		LLMModel:        model,
		LLMTier:         getEnv("LLM_TIER", "free"),
		MaxIterations:   maxIter,
		NATSUrl:         getEnv("NATS_URL", "nats://localhost:4222"),
		ShutdownTimeout: shutdownTimeout,
		ToolExecTimeout: toolExecTimeout,

		MongoURI: getEnv("MONGO_URI", "mongodb://localhost:27017"),
		MongoDB:  getEnv("MONGO_DB", "onevoice"),

		OpenRouterAPIKey: os.Getenv("OPENROUTER_API_KEY"),
		OpenAIAPIKey:     os.Getenv("OPENAI_API_KEY"),
		AnthropicAPIKey:  os.Getenv("ANTHROPIC_API_KEY"),

		SelfHostedEndpoints: parseIndexedEndpoints(),
	}, nil
}

// RedactMongoURI returns the Mongo connection URI with any embedded user:
// password stripped, suitable for logging on startup. Implementation is
// intentionally conservative: if the URI fails to parse it returns the
// string `<mongo-uri-redacted>` rather than leaking the raw value.
func (c *Config) RedactMongoURI() string {
	// Supported forms: mongodb://user:pass@host[:port]/db and
	// mongodb+srv://user:pass@host/db. We only redact the user-info segment
	// between "//" and "@"; everything after "@" is host/path which is
	// non-sensitive.
	uri := c.MongoURI
	if uri == "" {
		return ""
	}
	// Find the scheme separator.
	schemeEnd := -1
	for i := 0; i+2 < len(uri); i++ {
		if uri[i] == ':' && uri[i+1] == '/' && uri[i+2] == '/' {
			schemeEnd = i + 3
			break
		}
	}
	if schemeEnd < 0 {
		return "<mongo-uri-redacted>"
	}
	atIdx := -1
	for i := schemeEnd; i < len(uri); i++ {
		switch uri[i] {
		case '@':
			atIdx = i
		case '/':
			if atIdx < 0 {
				// Path started before any '@' — no user-info segment. Safe as-is.
				return uri
			}
		}
	}
	if atIdx < 0 {
		// No user-info segment. Safe as-is.
		return uri
	}
	return uri[:schemeEnd] + "***:***@" + uri[atIdx+1:]
}

// parseIndexedEndpoints scans SELF_HOSTED_N_URL / _MODEL / _API_KEY env vars
// for N = 0, 1, 2, … stopping when SELF_HOSTED_N_URL is missing.
// Entries without MODEL are skipped.
func parseIndexedEndpoints() []SelfHostedEndpoint {
	var result []SelfHostedEndpoint
	for i := 0; ; i++ {
		prefix := fmt.Sprintf("SELF_HOSTED_%d_", i)
		url := os.Getenv(prefix + "URL")
		if url == "" {
			break
		}
		model := os.Getenv(prefix + "MODEL")
		if model == "" {
			continue
		}
		result = append(result, SelfHostedEndpoint{
			URL:    url,
			Model:  model,
			APIKey: os.Getenv(prefix + "API_KEY"),
		})
	}
	return result
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
