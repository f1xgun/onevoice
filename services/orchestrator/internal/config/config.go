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

	return &Config{
		Port:            getEnv("PORT", "8090"),
		LLMModel:        model,
		LLMTier:         getEnv("LLM_TIER", "free"),
		MaxIterations:   maxIter,
		NATSUrl:         getEnv("NATS_URL", "nats://localhost:4222"),
		ShutdownTimeout: shutdownTimeout,

		OpenRouterAPIKey: os.Getenv("OPENROUTER_API_KEY"),
		OpenAIAPIKey:     os.Getenv("OPENAI_API_KEY"),
		AnthropicAPIKey:  os.Getenv("ANTHROPIC_API_KEY"),

		SelfHostedEndpoints: parseIndexedEndpoints(),
	}, nil
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
