package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/config"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/handler"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/orchestrator"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/prompt"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/tools"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

func main() {
	log := logger.New("orchestrator")
	slog.SetDefault(log)

	cfg, err := config.Load()
	if err != nil {
		log.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Build LLM registry — register the configured model
	registry := llm.NewRegistry()
	registry.RegisterModelProvider(&llm.ModelProviderEntry{
		Model:        cfg.LLMModel,
		Provider:     "stub",
		HealthStatus: "healthy",
		Enabled:      true,
	})

	// Router with no real providers — stub mode for local dev without API keys
	router := llm.NewRouter(registry)

	// Tool registry (empty — tools registered when agents connect in Phase 3)
	toolRegistry := tools.NewRegistry()

	// Business context (static for now — will be loaded per-user in Phase 3)
	biz := prompt.BusinessContext{
		Name: os.Getenv("BUSINESS_NAME"),
		Now:  time.Now(),
	}

	// Orchestrator
	orch := orchestrator.NewWithOptions(router, toolRegistry, orchestrator.Options{
		MaxIterations: cfg.MaxIterations,
	})

	// HTTP handler
	chatHandler := handler.NewChatHandler(orch, biz)

	r := chi.NewRouter()
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)

	r.Post("/chat/{conversationID}", chatHandler.Chat)
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"status":"ok"}`))
	})

	addr := ":" + cfg.Port
	log.Info("orchestrator listening", "addr", addr)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Error("server error", "error", err)
		os.Exit(1)
	}
}
