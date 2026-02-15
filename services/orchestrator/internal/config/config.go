package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds orchestrator configuration loaded from environment.
type Config struct {
	Port          string
	LLMModel      string
	LLMTier       string
	MaxIterations int
	NATSUrl       string

	// LLM provider API keys (at least one must be set)
	OpenRouterAPIKey string
	OpenAIAPIKey     string
	AnthropicAPIKey  string

	// Business context defaults (can be overridden per-request in future)
	BusinessName        string
	BusinessCategory    string
	BusinessTone        string
	ActiveIntegrations  []string // e.g. ["telegram","vk","yandex_business"]
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

	activeIntegrations := parseCSV(os.Getenv("ACTIVE_INTEGRATIONS"))

	return &Config{
		Port:          getEnv("PORT", "8090"),
		LLMModel:      model,
		LLMTier:       getEnv("LLM_TIER", "free"),
		MaxIterations: maxIter,
		NATSUrl:       getEnv("NATS_URL", "nats://localhost:4222"),

		OpenRouterAPIKey: os.Getenv("OPENROUTER_API_KEY"),
		OpenAIAPIKey:     os.Getenv("OPENAI_API_KEY"),
		AnthropicAPIKey:  os.Getenv("ANTHROPIC_API_KEY"),

		BusinessName:       os.Getenv("BUSINESS_NAME"),
		BusinessCategory:   os.Getenv("BUSINESS_CATEGORY"),
		BusinessTone:       os.Getenv("BUSINESS_TONE"),
		ActiveIntegrations: activeIntegrations,
	}, nil
}

// parseCSV splits a comma-separated string, trimming spaces, ignoring empty tokens.
func parseCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			result = append(result, t)
		}
	}
	return result
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
