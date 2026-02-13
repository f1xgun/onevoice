package config

import (
	"fmt"
	"os"
)

// Config holds orchestrator configuration loaded from environment.
type Config struct {
	Port          string
	LLMModel      string
	LLMTier       string
	MaxIterations int
}

// Load reads config from environment variables.
func Load() (*Config, error) {
	model := os.Getenv("LLM_MODEL")
	if model == "" {
		return nil, fmt.Errorf("LLM_MODEL is required")
	}

	return &Config{
		Port:          getEnv("PORT", "8090"),
		LLMModel:      model,
		LLMTier:       getEnv("LLM_TIER", "free"),
		MaxIterations: 10,
	}, nil
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
