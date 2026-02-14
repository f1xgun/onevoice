package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds orchestrator configuration loaded from environment.
type Config struct {
	Port          string
	LLMModel      string
	LLMTier       string
	MaxIterations int
	NATSUrl       string
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

	return &Config{
		Port:          getEnv("PORT", "8090"),
		LLMModel:      model,
		LLMTier:       getEnv("LLM_TIER", "free"),
		MaxIterations: maxIter,
		NATSUrl:       getEnv("NATS_URL", "nats://localhost:4222"),
	}, nil
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
