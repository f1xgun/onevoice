package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	natslib "github.com/nats-io/nats.go"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/llm/providers"
	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/config"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/handler"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/natsexec"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/orchestrator"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/prompt"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/tools"
)

func main() {
	log := logger.New("orchestrator")
	slog.SetDefault(log)

	cfg, err := config.Load()
	if err != nil {
		log.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	if err := run(log, cfg); err != nil {
		log.Error("application error", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger, cfg *config.Config) error {
	// Build LLM registry + wire real providers
	registry := llm.NewRegistry()
	routerOpts := buildProviderOpts(cfg, registry, log)
	if len(routerOpts) == 0 {
		return fmt.Errorf("no LLM provider API key set — set OPENROUTER_API_KEY, OPENAI_API_KEY, or ANTHROPIC_API_KEY")
	}
	router := llm.NewRouter(registry, routerOpts...)

	// Tool registry — wire NATS executors if NATS is available
	toolRegistry := tools.NewRegistry()
	nc, natsErr := natslib.Connect(cfg.NATSUrl)
	if natsErr != nil {
		log.Warn("NATS unavailable — tools will return stubs", "url", cfg.NATSUrl, "error", natsErr)
	} else {
		defer nc.Close()
		log.Info("connected to NATS", "url", cfg.NATSUrl)
		registerPlatformTools(toolRegistry, nc)
	}

	// Business context
	biz := prompt.BusinessContext{
		Name:               cfg.BusinessName,
		Category:           cfg.BusinessCategory,
		Tone:               cfg.BusinessTone,
		ActiveIntegrations: cfg.ActiveIntegrations,
		Now:                time.Now(),
	}
	if len(biz.ActiveIntegrations) == 0 {
		log.Warn("ACTIVE_INTEGRATIONS not set — LLM will have no platform tools available")
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
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	addr := ":" + cfg.Port
	log.Info("orchestrator listening", "addr", addr)

	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0, // SSE requires long-lived connections
	}
	if err := srv.ListenAndServe(); err != nil {
		return fmt.Errorf("server error: %w", err)
	}
	return nil
}

// registerPlatformTools wires NATS executors into the tool registry for each MVP agent.
// MVP platforms: Telegram (API), VK (API), Yandex.Business (RPA).
func registerPlatformTools(reg *tools.Registry, nc *natslib.Conn) {
	agents := []struct {
		id    a2a.AgentID
		tools []llm.ToolDefinition
	}{
		{
			id: a2a.AgentTelegram,
			tools: []llm.ToolDefinition{
				{Type: "function", Function: llm.FunctionDefinition{
					Name:        "telegram__send_channel_post",
					Description: "Публикует сообщение в Telegram-канал",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"text":       map[string]interface{}{"type": "string", "description": "Текст сообщения"},
							"channel_id": map[string]interface{}{"type": "string", "description": "ID канала"},
						},
						"required": []string{"text"},
					},
				}},
				{Type: "function", Function: llm.FunctionDefinition{
					Name:        "telegram__send_notification",
					Description: "Отправляет личное уведомление владельцу бизнеса в Telegram",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"text": map[string]interface{}{"type": "string", "description": "Текст уведомления"},
						},
						"required": []string{"text"},
					},
				}},
			},
		},
		{
			id: a2a.AgentVK,
			tools: []llm.ToolDefinition{
				{Type: "function", Function: llm.FunctionDefinition{
					Name:        "vk__publish_post",
					Description: "Публикует пост в сообщество ВКонтакте",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"text":     map[string]interface{}{"type": "string", "description": "Текст поста"},
							"group_id": map[string]interface{}{"type": "string", "description": "ID сообщества"},
						},
						"required": []string{"text"},
					},
				}},
				{Type: "function", Function: llm.FunctionDefinition{
					Name:        "vk__update_group_info",
					Description: "Обновляет информацию о сообществе ВКонтакте (описание, ссылки, контакты)",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"group_id":    map[string]interface{}{"type": "string", "description": "ID сообщества"},
							"description": map[string]interface{}{"type": "string", "description": "Новое описание"},
						},
						"required": []string{"group_id"},
					},
				}},
				{Type: "function", Function: llm.FunctionDefinition{
					Name:        "vk__get_comments",
					Description: "Получает комментарии к постам сообщества ВКонтакте",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"group_id": map[string]interface{}{"type": "string", "description": "ID сообщества"},
							"count":    map[string]interface{}{"type": "integer", "description": "Количество комментариев (макс 100)"},
						},
						"required": []string{"group_id"},
					},
				}},
			},
		},
		{
			id: a2a.AgentYandexBusiness,
			tools: []llm.ToolDefinition{
				{Type: "function", Function: llm.FunctionDefinition{
					Name:        "yandex_business__update_hours",
					Description: "Обновляет часы работы в Яндекс Бизнес (включая праздничные дни)",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"hours": map[string]interface{}{"type": "string", "description": "Часы работы в формате JSON"},
						},
						"required": []string{"hours"},
					},
				}},
				{Type: "function", Function: llm.FunctionDefinition{
					Name:        "yandex_business__update_info",
					Description: "Обновляет контактную информацию в Яндекс Бизнес (телефон, сайт, описание)",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"phone":       map[string]interface{}{"type": "string", "description": "Номер телефона"},
							"website":     map[string]interface{}{"type": "string", "description": "URL сайта"},
							"description": map[string]interface{}{"type": "string", "description": "Описание организации"},
						},
					},
				}},
				{Type: "function", Function: llm.FunctionDefinition{
					Name:        "yandex_business__get_reviews",
					Description: "Получает отзывы об организации из Яндекс Бизнес",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"limit": map[string]interface{}{"type": "integer", "description": "Количество отзывов (макс 50)"},
						},
					},
				}},
				{Type: "function", Function: llm.FunctionDefinition{
					Name:        "yandex_business__reply_review",
					Description: "Публикует ответ на отзыв в Яндекс Бизнес",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"review_id": map[string]interface{}{"type": "string", "description": "ID отзыва"},
							"text":      map[string]interface{}{"type": "string", "description": "Текст ответа"},
						},
						"required": []string{"review_id", "text"},
					},
				}},
			},
		},
	}

	conn := natsexec.NewNATSConn(nc)
	for _, a := range agents {
		exec := natsexec.New(a.id, conn)
		for _, def := range a.tools {
			reg.Register(def, exec)
		}
	}
}

// buildProviderOpts creates RouterOptions for every API key that is set in config,
// and registers the LLM model → provider mapping in the registry for each.
// Returns at least one option if any key is set, nil if none.
func buildProviderOpts(cfg *config.Config, reg *llm.Registry, log *slog.Logger) []llm.RouterOption {
	type providerSpec struct {
		name    string
		apiKey  string
		factory func(string) llm.Provider
	}

	specs := []providerSpec{
		{"openrouter", cfg.OpenRouterAPIKey, func(k string) llm.Provider { return providers.NewOpenRouter(k) }},
		{"openai", cfg.OpenAIAPIKey, func(k string) llm.Provider { return providers.NewOpenAI(k) }},
		{"anthropic", cfg.AnthropicAPIKey, func(k string) llm.Provider { return providers.NewAnthropic(k) }},
	}

	opts := make([]llm.RouterOption, 0, len(specs)+len(cfg.SelfHostedEndpoints))
	for _, spec := range specs {
		if spec.apiKey == "" {
			continue
		}
		p := spec.factory(spec.apiKey)
		opts = append(opts, llm.WithProvider(p))
		reg.RegisterModelProvider(&llm.ModelProviderEntry{
			Model:        cfg.LLMModel,
			Provider:     spec.name,
			HealthStatus: "healthy",
			Enabled:      true,
		})
		log.Info("LLM provider registered", "provider", spec.name, "model", cfg.LLMModel)
	}

	// Wire self-hosted endpoints
	for i, ep := range cfg.SelfHostedEndpoints {
		name := fmt.Sprintf("selfhosted-%d", i)
		p := providers.NewSelfHosted(name, ep.URL, ep.APIKey)
		if p == nil {
			log.Warn("self-hosted endpoint skipped (empty name or URL)", "index", i)
			continue
		}
		opts = append(opts, llm.WithProvider(p))
		reg.RegisterModelProvider(&llm.ModelProviderEntry{
			Model:        ep.Model,
			Provider:     name,
			HealthStatus: "healthy",
			Enabled:      true,
		})
		log.Info("self-hosted LLM registered", "name", name, "url", ep.URL, "model", ep.Model)
	}

	return opts
}
