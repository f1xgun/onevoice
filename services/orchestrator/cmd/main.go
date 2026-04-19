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
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.mongodb.org/mongo-driver/v2/mongo"
	mongoopts "go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/health"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/llm/providers"
	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/pkg/metrics"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/config"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/handler"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/natsexec"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/orchestrator"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/repository"
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

	// HITL-01: orchestrator-side Mongo connection (Plan 16-02 Task 2).
	// The orchestrator inserts pending_tool_calls rows at pause time and
	// marks calls dispatched after each NATS reply; it needs its own Mongo
	// connection (circular-dep avoidance — orchestrator cannot call the
	// API which already serves / consumes the orchestrator). The repo
	// variable is constructed here and threaded into orchestrator.New in
	// Plan 16-05 — for 16-02 it is sufficient that the dial succeeds and
	// the repo type exists.
	mongoCtx, mongoCancel := context.WithTimeout(context.Background(), 10*time.Second)
	mongoClient, err := mongo.Connect(mongoopts.Client().ApplyURI(cfg.MongoURI))
	if err != nil {
		mongoCancel()
		log.Error("orchestrator: failed to connect to mongo", "uri", cfg.RedactMongoURI(), "error", err)
		return fmt.Errorf("mongo connect: %w", err)
	}
	if pingErr := mongoClient.Ping(mongoCtx, nil); pingErr != nil {
		mongoCancel()
		log.Error("orchestrator: mongo ping failed", "uri", cfg.RedactMongoURI(), "error", pingErr)
		return fmt.Errorf("mongo ping: %w", pingErr)
	}
	mongoCancel()
	defer func() {
		shutdownMongoCtx, shutdownMongoCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownMongoCancel()
		_ = mongoClient.Disconnect(shutdownMongoCtx)
	}()
	mongoDB := mongoClient.Database(cfg.MongoDB)
	log.Info("orchestrator: connected to mongo", "uri", cfg.RedactMongoURI(), "db", cfg.MongoDB)
	pendingToolCallRepo := repository.NewPendingToolCallRepository(mongoDB)
	_ = pendingToolCallRepo // wired into orchestrator.New in Plan 16-05

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
	hc.AddCheck("mongo", func(ctx context.Context) error {
		return mongoClient.Ping(ctx, nil)
	})

	// Orchestrator
	orch := orchestrator.NewWithOptions(router, toolRegistry, orchestrator.Options{
		MaxIterations: cfg.MaxIterations,
	})

	// HTTP handler — business context is now provided per-request in the body
	chatHandler := handler.NewChatHandler(orch, cfg.LLMModel)

	// POLICY-07: cluster-internal endpoint serving the live tool registry
	// snapshot. The API service hits this at boot to validate every
	// business.settings.tool_approvals + project.approval_overrides entry
	// against the set of actually-registered tool names, logging
	// tool_approval_whitelist_unknown for drift.
	internalToolsHandler := handler.NewInternalToolsHandler(toolRegistry)

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
	r.Use(metrics.HTTPMiddleware)

	r.Post("/chat/{conversationID}", chatHandler.Chat)
	r.Get("/internal/tools/names", internalToolsHandler.Names)
	r.Handle("/metrics", promhttp.Handler())
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

// toolSpec binds a tool definition to its ToolFloor baseline (POLICY-01) and
// per-tool EditableFields allowlist (HITL-L4 promoted into v1.3 per D-10/D-11).
//
// Policy guidelines for choosing a floor:
//   - ToolFloorAuto       — read-only / safe queries (no external side effects).
//   - ToolFloorManual     — any public mutation (post, reply, update, schedule,
//                           upload). Editable allowlist covers ONLY
//                           human-facing text fields (text/caption/description);
//                           ids, recipients, URLs, dates, categories, and
//                           quantities are pinned at pause time.
//   - ToolFloorForbidden  — destructive operations that cannot be recovered
//                           via the UI (e.g., vk__delete_comment). Kept
//                           registered so the LLM sees it exists, but always
//                           blocked by policy. Operator must lift via code
//                           review + redeploy, never via settings.
//
// When in doubt, prefer manual + a narrow editable list (conservative default).
type toolSpec struct {
	def      llm.ToolDefinition
	floor    domain.ToolFloor
	editable []string
}

// registerPlatformTools wires NATS executors into the tool registry for each MVP agent.
// MVP platforms: Telegram (API), VK (API), Yandex.Business (RPA), Google Business (API).
//
// Every tool registration is explicit — Register takes floor + editableFields
// as required arguments so a newly-added tool can never silently inherit
// ToolFloorAuto. See toolSpec above for the policy rubric used below.
func registerPlatformTools(reg *tools.Registry, nc *natslib.Conn) {
	agents := []struct {
		id    a2a.AgentID
		tools []toolSpec
	}{
		{
			id: a2a.AgentTelegram,
			tools: []toolSpec{
				// Mutating public: posts to a Telegram channel. text + parse_mode
				// editable; channel_id pinned from integration.
				{
					def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
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
					floor:    domain.ToolFloorManual,
					editable: []string{"text"},
				},
				// Mutating public: posts a photo + caption. caption editable;
				// photo_url and channel_id pinned (redirecting either at edit
				// time would be a HITL-07 footgun).
				{
					def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
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
					floor:    domain.ToolFloorManual,
					editable: []string{"caption"},
				},
				// DM notification to owner. text editable; recipient pinned
				// from the integration (never editable).
				{
					def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
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
					floor:    domain.ToolFloorManual,
					editable: []string{"text"},
				},
				// Read-only query of recent messages. Auto, no edit needed.
				{
					def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
						Name:        "telegram__get_reviews",
						Description: "Получает последние сообщения/отзывы, отправленные боту или в канал через Telegram. Каждое сообщение содержит поля message_id и chat_id — используй их для ответа через telegram__reply_to_comment.",
						Parameters: map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"limit": map[string]interface{}{"type": "integer", "description": "Количество сообщений (макс 100)"},
							},
						},
					}},
					floor:    domain.ToolFloorAuto,
					editable: nil,
				},
				// Mutating public: replies to a comment. text editable;
				// message_id + chat_id + channel_id pinned (changing these
				// would redirect the reply to an unrelated conversation).
				{
					def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
						Name:        "telegram__reply_to_comment",
						Description: "Отвечает на конкретный комментарий или сообщение в Telegram. Используй эту функцию когда нужно ответить на комментарий — НЕ используй telegram__send_channel_post для ответов на комментарии.",
						Parameters: map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"message_id": map[string]interface{}{"type": "integer", "description": "ID сообщения/комментария, на который отвечаем (поле message_id из telegram__get_reviews)"},
								"chat_id":    map[string]interface{}{"type": "string", "description": "ID чата/группы обсуждений, где находится комментарий (поле chat_id из telegram__get_reviews)"},
								"text":       map[string]interface{}{"type": "string", "description": "Текст ответа"},
								"channel_id": map[string]interface{}{"type": "string", "description": "ID канала (необязательно, для выбора интеграции)"},
							},
							"required": []string{"message_id", "chat_id", "text"},
						},
					}},
					floor:    domain.ToolFloorManual,
					editable: []string{"text"},
				},
			},
		},
		{
			id: a2a.AgentVK,
			tools: []toolSpec{
				// Mutating public: publishes wall post. text editable; group_id pinned.
				{
					def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
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
					floor:    domain.ToolFloorManual,
					editable: []string{"text"},
				},
				// Mutating public: photo + caption. caption editable; photo_url + group_id pinned.
				{
					def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
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
					floor:    domain.ToolFloorManual,
					editable: []string{"caption"},
				},
				// Mutating public w/ scheduled release. text editable;
				// publish_date NOT editable (changing a scheduled time is a
				// semantic change — a separate tool call makes intent explicit).
				{
					def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
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
					floor:    domain.ToolFloorManual,
					editable: []string{"text"},
				},
				// Mutating public: group meta update. description editable;
				// group_id pinned. Contacts/links intentionally omitted from
				// edit-allowlist until the LLM's JSON schema exposes them.
				{
					def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
						Name:        "vk__update_group_info",
						Description: "Обновляет информацию о сообществе ВКонтакте (описание, ссылки, контакты). Если group_id не указан, используется сообщество из активной VK-интеграции.",
						Parameters: map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"group_id":    map[string]interface{}{"type": "string", "description": "Числовой ID сообщества ВКонтакте. Необязателен — берётся из активной интеграции."},
								"description": map[string]interface{}{"type": "string", "description": "Новое описание"},
							},
							"required": []string{},
						},
					}},
					floor:    domain.ToolFloorManual,
					editable: []string{"description"},
				},
				// Read-only. Auto.
				{
					def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
						Name:        "vk__get_comments",
						Description: "Получает комментарии к конкретному посту на стене сообщества ВКонтакте. Если post_id не указан, возвращает комментарии к последнему посту.",
						Parameters: map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"post_id":  map[string]interface{}{"type": "integer", "description": "ID поста на стене. Если не указан — берётся последний пост."},
								"group_id": map[string]interface{}{"type": "string", "description": "Числовой ID сообщества ВКонтакте. Необязателен — берётся из активной интеграции."},
								"count":    map[string]interface{}{"type": "integer", "description": "Количество комментариев (макс 100)"},
							},
							"required": []string{},
						},
					}},
					floor:    domain.ToolFloorAuto,
					editable: nil,
				},
				// Mutating public: comment reply. text editable; ids pinned.
				{
					def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
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
					floor:    domain.ToolFloorManual,
					editable: []string{"text"},
				},
				// Destructive: hard-deletes a comment. Forbidden — the LLM
				// cannot request this even behind approval. Lifting requires
				// a deliberate code change.
				{
					def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
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
					floor:    domain.ToolFloorForbidden,
					editable: nil,
				},
				// Read-only. Auto.
				{
					def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
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
					floor:    domain.ToolFloorAuto,
					editable: nil,
				},
				// Read-only. Auto.
				{
					def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
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
					floor:    domain.ToolFloorAuto,
					editable: nil,
				},
			},
		},
		{
			id: a2a.AgentYandexBusiness,
			tools: []toolSpec{
				// Read-only. Auto.
				{
					def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
						Name:        "yandex_business__get_info",
						Description: "Получает текущую информацию об организации в Яндекс Бизнес: название, телефон, email, часы работы, адрес, статус.",
						Parameters: map[string]interface{}{
							"type":       "object",
							"properties": map[string]interface{}{},
						},
					}},
					floor:    domain.ToolFloorAuto,
					editable: nil,
				},
				// Mutating public: hours. hours editable (text payload).
				{
					def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
						Name:        "yandex_business__update_hours",
						Description: "Обновляет часы работы в Яндекс Бизнес. Принимает описание расписания в свободном формате.",
						Parameters: map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"hours": map[string]interface{}{"type": "string", "description": "Часы работы в формате JSON"},
							},
							"required": []string{"hours"},
						},
					}},
					floor:    domain.ToolFloorManual,
					editable: []string{"hours"},
				},
				// Mutating public: business-profile text fields. description
				// editable. phone + website pinned (dialing/URL redirection is
				// a high-impact mutation; operator confirms via UI toggle before
				// any tool call rather than post-hoc edit).
				{
					def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
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
					floor:    domain.ToolFloorManual,
					editable: []string{"description"},
				},
				// Read-only. Auto.
				{
					def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
						Name:        "yandex_business__get_reviews",
						Description: "Получает отзывы об организации из Яндекс Бизнес",
						Parameters: map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"limit": map[string]interface{}{"type": "integer", "description": "Количество отзывов (макс 50)"},
							},
						},
					}},
					floor:    domain.ToolFloorAuto,
					editable: nil,
				},
				// Mutating public: review reply. text editable; review_id pinned.
				{
					def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
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
					floor:    domain.ToolFloorManual,
					editable: []string{"text"},
				},
				// Mutating public: upload photo. Nothing editable (category
				// and photo_url are both semantic — editing either changes
				// what the operator sees in the card vs what actually uploads).
				{
					def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
						Name:        "yandex_business__upload_photo",
						Description: "Загружает фото в Яндекс Бизнес. Категория: general (общее), logo (логотип), services, interior, exterior, enter (вход), goods (товары).",
						Parameters: map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"photo_url": map[string]interface{}{"type": "string", "description": "Публичный URL изображения для загрузки"},
								"category":  map[string]interface{}{"type": "string", "description": "Категория фото: general, logo, services, interior, exterior, enter, goods"},
							},
							"required": []string{"photo_url"},
						},
					}},
					floor:    domain.ToolFloorManual,
					editable: nil,
				},
				// Mutating public: publication. text editable.
				{
					def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
						Name:        "yandex_business__create_post",
						Description: "Создаёт публикацию (пост) в Яндекс Бизнес. Публикация появится в Поиске Яндекса и Яндекс Картах.",
						Parameters: map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"text": map[string]interface{}{"type": "string", "description": "Текст публикации"},
							},
							"required": []string{"text"},
						},
					}},
					floor:    domain.ToolFloorManual,
					editable: []string{"text"},
				},
			},
		},
		{
			id: a2a.AgentGoogleBusiness,
			tools: []toolSpec{
				// Read-only. Auto.
				{
					def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
						Name:        "google_business__get_reviews",
						Description: "Получает отзывы о локации из Google Business Profile. Возвращает список отзывов с рейтингами, комментариями и ответами владельца.",
						Parameters: map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"limit": map[string]interface{}{"type": "integer", "description": "Количество отзывов (макс 50)"},
							},
						},
					}},
					floor:    domain.ToolFloorAuto,
					editable: nil,
				},
				// Mutating public: review reply. text editable; review_name pinned.
				{
					def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
						Name:        "google_business__reply_review",
						Description: "Отвечает на отзыв в Google Business Profile от имени владельца бизнеса.",
						Parameters: map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"review_name": map[string]interface{}{"type": "string", "description": "Полное имя ресурса отзыва (поле name из google_business__get_reviews)"},
								"text":        map[string]interface{}{"type": "string", "description": "Текст ответа на отзыв"},
							},
							"required": []string{"review_name", "text"},
						},
					}},
					floor:    domain.ToolFloorManual,
					editable: []string{"text"},
				},
			},
		},
	}

	conn := natsexec.NewNATSConn(nc)
	for _, a := range agents {
		for _, spec := range a.tools {
			exec := natsexec.New(a.id, spec.def.Function.Name, conn)
			reg.Register(spec.def, exec, spec.floor, spec.editable)
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
