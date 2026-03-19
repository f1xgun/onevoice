package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	natslib "github.com/nats-io/nats.go"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/health"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/llm/providers"
	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/config"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/handler"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/natsexec"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/orchestrator"
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
		log.Info("connected to NATS", "url", cfg.NATSUrl)
		registerPlatformTools(toolRegistry, nc)
	}

	// Health checker
	hc := health.New()
	if nc != nil {
		hc.AddCheck("nats", func(ctx context.Context) error {
			if !nc.IsConnected() {
				return fmt.Errorf("nats disconnected")
			}
			return nil
		})
	}

	// Orchestrator
	orch := orchestrator.NewWithOptions(router, toolRegistry, orchestrator.Options{
		MaxIterations: cfg.MaxIterations,
	})

	// HTTP handler — business context is now provided per-request in the body
	chatHandler := handler.NewChatHandler(orch, cfg.LLMModel)

	r := chi.NewRouter()
	r.Use(chimiddleware.RequestID)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			if corrID := r.Header.Get("X-Correlation-ID"); corrID != "" {
				ctx = logger.WithCorrelationID(ctx, corrID)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)

	r.Post("/chat/{conversationID}", chatHandler.Chat)
	r.Get("/health/live", hc.LiveHandler())
	r.Get("/health/ready", hc.ReadyHandler())
	r.Get("/health", hc.LiveHandler()) // backward compat

	addr := ":" + cfg.Port

	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0, // SSE requires long-lived connections
	}

	// Start server in goroutine
	errCh := make(chan error, 1)
	go func() {
		log.Info("orchestrator listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	// Wait for signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-quit:
		log.Info("shutting down orchestrator")
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	// 1. Stop HTTP server (drains active SSE connections)
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("http shutdown error", "error", err)
	}

	// 2. Drain NATS
	if nc != nil {
		_ = nc.Drain()
	}

	log.Info("orchestrator stopped")
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
					Description: "Публикует текстовое сообщение в Telegram-канал (без фото). Если нужно опубликовать пост с фото — используй telegram__send_channel_photo вместо этого.",
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
					Name:        "telegram__send_channel_photo",
					Description: "Публикует пост с фото и текстовой подписью в Telegram-канал. Используй эту функцию вместо send_channel_post когда нужно опубликовать пост с изображением.",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"photo_url":  map[string]interface{}{"type": "string", "description": "Публичный URL изображения"},
							"caption":    map[string]interface{}{"type": "string", "description": "Подпись к фото"},
							"channel_id": map[string]interface{}{"type": "string", "description": "ID канала"},
						},
						"required": []string{"photo_url"},
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
					Description: "Публикует текстовый пост (без фото) на стену сообщества ВКонтакте. Если нужно опубликовать пост с фото — используй vk__post_photo вместо этого.",
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
					Name:        "vk__post_photo",
					Description: "Публикует пост с фото и текстовой подписью на стену сообщества ВКонтакте. Используй эту функцию вместо publish_post когда нужно опубликовать пост с изображением.",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"photo_url": map[string]interface{}{"type": "string", "description": "Публичный URL изображения для загрузки"},
							"caption":   map[string]interface{}{"type": "string", "description": "Текстовая подпись к фото"},
							"group_id":  map[string]interface{}{"type": "string", "description": "ID сообщества ВКонтакте"},
						},
						"required": []string{"photo_url"},
					},
				}},
				{Type: "function", Function: llm.FunctionDefinition{
					Name:        "vk__schedule_post",
					Description: "Планирует отложенный пост на стене сообщества ВКонтакте. Пост будет автоматически опубликован ВКонтакте в указанное время.",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"text":         map[string]interface{}{"type": "string", "description": "Текст поста"},
							"publish_date": map[string]interface{}{"type": "string", "description": "Дата и время публикации (Unix timestamp или ISO 8601 формат, например 2026-03-20T12:00:00Z)"},
							"group_id":     map[string]interface{}{"type": "string", "description": "ID сообщества ВКонтакте"},
						},
						"required": []string{"text", "publish_date"},
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
				{Type: "function", Function: llm.FunctionDefinition{
					Name:        "vk__reply_comment",
					Description: "Отвечает на комментарий к посту на стене сообщества ВКонтакте. Создает ответ в ветке обсуждения.",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"post_id":    map[string]interface{}{"type": "number", "description": "ID поста на стене"},
							"comment_id": map[string]interface{}{"type": "number", "description": "ID комментария, на который нужно ответить"},
							"text":       map[string]interface{}{"type": "string", "description": "Текст ответа на комментарий"},
							"group_id":   map[string]interface{}{"type": "string", "description": "ID сообщества ВКонтакте"},
						},
						"required": []string{"post_id", "comment_id", "text"},
					},
				}},
				{Type: "function", Function: llm.FunctionDefinition{
					Name:        "vk__delete_comment",
					Description: "Удаляет комментарий к посту на стене сообщества ВКонтакте. Требуются права администратора или модератора сообщества.",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"comment_id": map[string]interface{}{"type": "number", "description": "ID комментария для удаления"},
							"group_id":   map[string]interface{}{"type": "string", "description": "ID сообщества ВКонтакте"},
						},
						"required": []string{"comment_id"},
					},
				}},
				{Type: "function", Function: llm.FunctionDefinition{
					Name:        "vk__get_community_info",
					Description: "Получает информацию о сообществе ВКонтакте: название, описание, количество подписчиков, статус, ссылки. Используй для ответа на вопросы о сообществе.",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"group_id": map[string]interface{}{"type": "string", "description": "ID сообщества ВКонтакте. Если не указан, используется группа из активной VK-интеграции."},
						},
						"required": []string{},
					},
				}},
				{Type: "function", Function: llm.FunctionDefinition{
					Name:        "vk__get_wall_posts",
					Description: "Получает последние посты со стены сообщества ВКонтакте с данными о лайках, комментариях, репостах и просмотрах.",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"group_id": map[string]interface{}{"type": "string", "description": "ID сообщества ВКонтакте. Если не указан, используется группа из активной VK-интеграции."},
							"count":    map[string]interface{}{"type": "integer", "description": "Количество постов (по умолчанию 10, макс 100)"},
						},
						"required": []string{},
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
		for _, def := range a.tools {
			exec := natsexec.New(a.id, def.Function.Name, conn)
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
