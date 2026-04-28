package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	natslib "github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/f1xgun/onevoice/pkg/crypto"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/health"
	"github.com/f1xgun/onevoice/pkg/hitlvalidation"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/llm/providers"
	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/services/api/internal/config"
	"github.com/f1xgun/onevoice/services/api/internal/handler"
	"github.com/f1xgun/onevoice/services/api/internal/platform"
	"github.com/f1xgun/onevoice/services/api/internal/repository"
	"github.com/f1xgun/onevoice/services/api/internal/router"
	"github.com/f1xgun/onevoice/services/api/internal/service"
	"github.com/f1xgun/onevoice/services/api/internal/storage"
	"github.com/f1xgun/onevoice/services/api/internal/taskhub"
)

func main() {
	// Initialize logger
	log := logger.New("api")
	slog.SetDefault(log)

	// Load configuration
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
	log.Info("starting onevoice api server")

	// Initialize database connections
	ctx := context.Background()

	// PostgreSQL
	pgConnStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		cfg.PostgresUser, cfg.PostgresPass, cfg.PostgresHost, cfg.PostgresPort, cfg.PostgresDB)
	pgPool, err := pgxpool.New(ctx, pgConnStr)
	if err != nil {
		return fmt.Errorf("connect to postgres: %w", err)
	}
	defer pgPool.Close()
	log.Info("connected to postgres")

	// MongoDB
	mongoClient, err := mongo.Connect(options.Client().ApplyURI(cfg.MongoURI))
	if err != nil {
		return fmt.Errorf("connect to mongodb: %w", err)
	}
	defer func() { _ = mongoClient.Disconnect(ctx) }()
	mongoDB := mongoClient.Database(cfg.MongoDB)
	log.Info("connected to mongodb")

	// Phase 15 Mongo backfill — idempotent, marker-gated. Must complete
	// before we serve traffic so every pre-existing conversation has the
	// fields the sidebar and move-chat rely on. Bounded to 30s so a
	// broken Mongo does not hang startup forever.
	backfillCtx, backfillCancel := context.WithTimeout(ctx, 30*time.Second)
	if err := repository.BackfillConversationsV15(backfillCtx, mongoDB); err != nil {
		backfillCancel()
		slog.ErrorContext(backfillCtx, "phase 15 backfill failed", "error", err)
		return fmt.Errorf("phase 15 backfill: %w", err)
	}
	backfillCancel()

	// Phase 19 Mongo backfill (Plan 19-02 / D-02) — pinned_at swap. Migrates
	// every conversation from the post-Phase-15 shape (legacy `pinned: bool`)
	// to the Phase-19 shape (`pinned_at: *time.Time`, no legacy field).
	// Three steps: (1) pinned_at = nil for missing field, (2) legacy
	// pinned:true → pinned_at = updated_at, (3) $unset legacy pinned bool.
	// Idempotent via schema_migrations marker (same shape as the V15 backfill
	// above). Bounded to 30s. BLOCKING: 19-02 must wire this before serving
	// traffic so the new ConversationRepository.Pin/Unpin atomic methods
	// operate against a uniform schema across pre- and post-Phase-19 data.
	backfillCtx2, backfillCancel2 := context.WithTimeout(ctx, 30*time.Second)
	if err := repository.BackfillConversationsV19(backfillCtx2, mongoDB); err != nil {
		backfillCancel2()
		slog.ErrorContext(backfillCtx2, "phase 19 backfill failed", "error", err)
		return fmt.Errorf("phase 19 backfill: %w", err)
	}
	backfillCancel2()

	// HITL-10: pending-tool-calls startup reconciliation.
	// Phase 16 Plan 16-02. Three things happen here, in order:
	//   1. EnsurePendingToolCallsIndexes — creates TTL on expires_at,
	//      compound (conversation_id, status), and business_id indexes.
	//      Idempotent: safe on every boot. HITL is broken without these
	//      indexes (the resolve handler would scan the whole collection)
	//      so we fail fast if creation errors.
	//   2. NewPendingToolCallRepository — constructs the repo used by
	//      chat_proxy / resolve / resume handlers in later plans (16-06,
	//      16-07). Wired into downstream consumers in those plans.
	//   3. ReconcileOrphanPreparing (goroutine, 30s bound) — one-shot
	//      sweep that marks "preparing" batches older than 5 minutes as
	//      "expired" (Pattern 3 crash recovery: orchestrator inserted a
	//      preparing row then crashed before PromoteToPending). Runs async
	//      so the HTTP server can bind immediately.
	indexesCtx, indexesCancel := context.WithTimeout(ctx, 30*time.Second)
	if err := repository.EnsurePendingToolCallsIndexes(indexesCtx, mongoDB); err != nil {
		indexesCancel()
		slog.ErrorContext(indexesCtx, "failed to ensure pending_tool_calls indexes", "error", err)
		return fmt.Errorf("ensure pending_tool_calls indexes: %w", err)
	}
	indexesCancel()

	// Phase 18 Plan 03 (D-08a) + Phase 19 Plan 19-02: compound indexes on
	// conversations. EnsureConversationIndexes manages BOTH the Phase-18
	// `conversations_user_biz_title_status` (auto-titler hot path) AND the
	// Phase-19 `conversations_user_biz_proj_pinned_recency` (sidebar list
	// ordering) indexes. Idempotent — safe on every boot.
	indexesCtx2, indexesCancel2 := context.WithTimeout(ctx, 30*time.Second)
	if err := repository.EnsureConversationIndexes(indexesCtx2, mongoDB); err != nil {
		indexesCancel2()
		slog.ErrorContext(indexesCtx2, "failed to ensure conversation indexes", "error", err)
		return fmt.Errorf("ensure conversation indexes: %w", err)
	}
	indexesCancel2()

	// Phase 19 Plan 19-03 / SEARCH-01 / SEARCH-06 — text indexes for
	// sidebar search. Two text indexes are created idempotently:
	//   - conversations_title_text_v19  (default_language: russian, weight 20)
	//   - messages_content_text_v19     (default_language: russian, weight 10)
	//
	// 60s timeout (vs the 30s used for compound indexes) because text-index
	// builds on a non-empty corpus take longer than equality-key indexes.
	//
	// CRITICAL ORDERING (T-19-INDEX-503 mitigation): the readiness flag
	// on the Searcher MUST be flipped only AFTER this call returns nil.
	// The Searcher is constructed below in the service-wiring block; the
	// readiness flip fires there. The atomic.Bool.Store provides a
	// happens-before edge against every subsequent Load by handler
	// goroutines.
	indexesCtx3, indexesCancel3 := context.WithTimeout(ctx, 60*time.Second)
	if err := repository.EnsureSearchIndexes(indexesCtx3, mongoDB); err != nil {
		indexesCancel3()
		slog.ErrorContext(indexesCtx3, "failed to ensure search text indexes", "error", err)
		return fmt.Errorf("ensure search indexes: %w", err)
	}
	indexesCancel3()
	pendingToolCallRepo := repository.NewPendingToolCallRepository(mongoDB)
	_ = pendingToolCallRepo // consumed by chat_proxy (16-06) and HITL handlers (16-07)
	go func() {
		sweepCtx, sweepCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer sweepCancel()
		n, reconcileErr := pendingToolCallRepo.ReconcileOrphanPreparing(sweepCtx, 5*time.Minute)
		if reconcileErr != nil {
			slog.ErrorContext(sweepCtx, "pending_tool_calls orphan reconcile failed", "error", reconcileErr)
			return
		}
		if n > 0 {
			slog.InfoContext(sweepCtx, "pending_tool_calls: reconciled orphan preparing batches", "count", n)
		}
	}()

	// POLICY-07: tool-approval startup validation.
	// Fires one HTTP GET against the orchestrator's /internal/tools/names,
	// reads every business.settings.tool_approvals and every
	// project.approval_overrides directly from Postgres, and logs
	// tool_approval_whitelist_unknown for entries referencing tools that no
	// longer exist in the live registry. Non-blocking: the sweep runs in a
	// goroutine, retries once after 5s on HTTP failure, and quietly skips
	// on second failure so a slow orchestrator boot does not gate the API.
	go runToolApprovalStartupValidation(ctx, pgPool, cfg.OrchestratorURL)

	// Redis
	redisClient := redis.NewClient(&redis.Options{
		Addr: fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
	})
	defer func() { _ = redisClient.Close() }()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("connect to redis: %w", err)
	}
	log.Info("connected to redis")

	// Initialize encryptor for token encryption
	enc, err := crypto.NewEncryptor([]byte(cfg.EncryptionKey))
	if err != nil {
		return fmt.Errorf("create encryptor: %w", err)
	}

	// Initialize repositories
	userRepo := repository.NewUserRepository(pgPool)
	businessRepo := repository.NewBusinessRepository(pgPool)
	integrationRepo := repository.NewIntegrationRepository(pgPool)
	conversationRepo := repository.NewConversationRepository(mongoDB)
	messageRepo := repository.NewMessageRepository(mongoDB)
	reviewRepo := repository.NewReviewRepository(mongoDB)
	postRepo := repository.NewPostRepository(mongoDB)
	agentTaskRepo := repository.NewAgentTaskRepository(mongoDB)

	// Phase 18 — Auto-titler LLM Router wiring (Plan 18-02).
	//
	// Graceful disable per Pitfall 1 / Assumption A6: when no LLM provider
	// key is set OR no model is configured, the titler is left nil and
	// downstream Plan 18-05's fireAutoTitleIfPending becomes a no-op. The
	// API service must boot in dev environments without any LLM env at all.
	//
	// `buildProviderOpts` is lifted verbatim from
	// services/orchestrator/cmd/main.go:740-788; the construction sequence
	// (NewRegistry → buildProviderOpts → NewRouter) mirrors orchestrator's
	// run() function lines 53-59. This is the FIRST pkg/llm import in
	// services/api (Phase 18 Landmine 3).
	//
	// `_ = llmRouter` is a deliberate placeholder — Plan 18-04 will replace
	// it with `service.NewTitler(llmRouter, conversationRepo, titlerModel)`
	// and thread the result into chat_proxy/handler.NewTitlerHandler.
	var llmRouter *llm.Router
	titlerModel := cfg.TitlerModel
	if titlerModel != "" {
		registry := llm.NewRegistry()
		routerOpts := buildProviderOpts(cfg, registry, log)
		if len(routerOpts) > 0 {
			llmRouter = llm.NewRouter(registry, routerOpts...)
			log.Info("auto-titler: llm router constructed", "model", titlerModel, "providers", len(routerOpts))
		} else {
			log.Warn("auto-titler: disabled (no LLM provider API key set; set OPENROUTER_API_KEY, OPENAI_API_KEY, or ANTHROPIC_API_KEY to enable)")
		}
	} else {
		log.Warn("auto-titler: disabled (TITLER_MODEL and LLM_MODEL both unset)")
	}

	// Phase 18 Plan 04 — construct the Titler service when the Router is
	// available. titler stays nil on the graceful-disable branch so Plan 05's
	// fireAutoTitleIfPending becomes a no-op (Pitfall 1 / Assumption A6).
	// llmRouter (concrete *llm.Router) implicitly satisfies the package-private
	// chatCaller interface in services/api/internal/service/titler.go via its
	// Chat method — Go's structural typing handles the conversion here.
	var titler *service.Titler
	if llmRouter != nil {
		titler = service.NewTitler(llmRouter, conversationRepo, titlerModel)
		log.Info("auto-titler: service constructed", "model", titlerModel)
	}
	// Phase 18 Plan 05 — TitlerHandler for POST /conversations/{id}/regenerate-title.
	// titler may be nil here (graceful disable per A6); the handler returns 503
	// in that case. conversationRepo + messageRepo are required (panic-on-nil).
	titlerHandler := handler.NewTitlerHandler(titler, conversationRepo, messageRepo)

	// Phase 19 Plan 19-03 — Search service + handler.
	//
	// CRITICAL ORDERING (T-19-INDEX-503 mitigation): MarkIndexesReady is
	// called HERE, AFTER repository.EnsureSearchIndexes has already
	// returned nil in the index-creation block above. The atomic.Bool's
	// Store ensures a happens-before edge against every subsequent Load
	// by handler goroutines (RESEARCH §7). Reordering this would cause
	// the readiness flag to flip before indexes exist — Searcher.Search
	// would no longer return ErrSearchIndexNotReady on a cold boot, and
	// queries would hit a missing $text index.
	//
	// Verified by the plan's BLOCKING grep ordering check:
	//   python3 -c "src=open('services/api/cmd/main.go').read();
	//               ei=src.find('EnsureSearchIndexes');
	//               mi=src.find('MarkIndexesReady');
	//               exit(0 if 0 < ei < mi else 1)"
	searcher := service.NewSearcher(conversationRepo, messageRepo)
	searcher.MarkIndexesReady()

	// In-process hub that fans out task lifecycle events to SSE subscribers.
	taskHub := taskhub.New()

	// Initialize services
	userService, err := service.NewUserService(userRepo, redisClient, cfg.JWTSecret)
	if err != nil {
		return fmt.Errorf("create user service: %w", err)
	}
	businessService := service.NewBusinessService(businessRepo)

	// Build Google token refresher if credentials are configured
	var refresher service.TokenRefresher
	if cfg.GoogleClientID != "" && cfg.GoogleClientSecret != "" {
		refresher = &googleTokenRefresher{
			clientID:     cfg.GoogleClientID,
			clientSecret: cfg.GoogleClientSecret,
			httpClient:   &http.Client{Timeout: 10 * time.Second},
		}
	}
	integrationService := service.NewIntegrationService(integrationRepo, enc, refresher)
	oauthService := service.NewOAuthService(redisClient)
	reviewService := service.NewReviewService(reviewRepo, businessService)
	postService := service.NewPostService(postRepo, businessService)
	agentTaskService := service.NewAgentTaskService(agentTaskRepo, businessService)

	// Initialize object storage (MinIO / S3) for user uploads
	objectStorage, err := storage.NewMinioClient(ctx, storage.Config{
		Endpoint:        cfg.S3Endpoint,
		AccessKey:       cfg.S3AccessKey,
		SecretKey:       cfg.S3SecretKey,
		Bucket:          cfg.S3Bucket,
		UseSSL:          cfg.S3UseSSL,
		PublicURLPrefix: cfg.S3PublicURLPrefix,
	})
	if err != nil {
		return fmt.Errorf("init object storage: %w", err)
	}
	log.Info("connected to object storage", "endpoint", cfg.S3Endpoint, "bucket", cfg.S3Bucket)

	// Platform syncer: pushes business info updates to connected platforms
	platformSyncer := platform.NewSyncer(
		&integrationSyncAdapter{svc: integrationService},
		nil,
		cfg.PublicURL,
	)
	platformSyncer.SetTaskRecorder(agentTaskRepo)
	platformSyncer.SetTaskHub(taskHub)

	// Initialize handlers
	oauthHandler := handler.NewOAuthHandler(oauthService, integrationService, businessService, handler.OAuthConfig{
		VKClientID:         cfg.VKClientID,
		VKClientSecret:     cfg.VKClientSecret,
		VKRedirectURI:      cfg.VKRedirectURI,
		VKServiceKey:       cfg.VKServiceKey,
		YandexClientID:     cfg.YandexClientID,
		YandexClientSecret: cfg.YandexClientSecret,
		YandexRedirectURI:  cfg.YandexRedirectURI,
		TelegramBotToken:   cfg.TelegramBotToken,
		GoogleClientID:     cfg.GoogleClientID,
		GoogleClientSecret: cfg.GoogleClientSecret,
		GoogleRedirectURI:  cfg.GoogleRedirectURI,
	}, nil, redisClient)
	internalTokenHandler := handler.NewInternalTokenHandler(integrationService)
	// chatProxyHandler is constructed after the Phase 15 project service is
	// wired below (the proxy enriches each request with the conversation's
	// project_* fields for orchestrator prompt layering + whitelist).

	authHandler, err := handler.NewAuthHandler(userService, cfg.SecureCookies)
	if err != nil {
		return fmt.Errorf("create auth handler: %w", err)
	}
	businessHandler, err := handler.NewBusinessHandler(businessService, platformSyncer, objectStorage)
	if err != nil {
		return fmt.Errorf("create business handler: %w", err)
	}
	integrationHandler, err := handler.NewIntegrationHandler(integrationService, businessService)
	if err != nil {
		return fmt.Errorf("create integration handler: %w", err)
	}
	// Conversation handler is constructed below (after the project service is
	// built) because Phase 15 extended its dependency set with business +
	// project services for create-conversation scoping and /move system-note.
	reviewHandler, err := handler.NewReviewHandler(reviewService)
	if err != nil {
		return fmt.Errorf("create review handler: %w", err)
	}
	postHandler, err := handler.NewPostHandler(postService)
	if err != nil {
		return fmt.Errorf("create post handler: %w", err)
	}
	agentTaskHandler, err := handler.NewAgentTaskHandler(agentTaskService, taskHub)
	if err != nil {
		return fmt.Errorf("create agent task handler: %w", err)
	}

	// Phase 15 Projects — three-line wiring through a single
	// domain.ProjectRepository interface value (HardDeleteCascade is part of
	// the interface per Plan 15-01). No type assertion, no anonymous
	// interface widening.
	projectRepo := repository.NewProjectRepository(pgPool, mongoDB)
	projectService := service.NewProjectService(projectRepo)
	projectHandler, err := handler.NewProjectHandler(projectService, businessService)
	if err != nil {
		return fmt.Errorf("create project handler: %w", err)
	}

	// Conversation handler depends on business + project services for Phase 15
	// create-conversation scoping and the /move endpoint system-note append.
	conversationHandler, err := handler.NewConversationHandler(conversationRepo, messageRepo, businessService, projectService, pendingToolCallRepo)
	if err != nil {
		return fmt.Errorf("create conversation handler: %w", err)
	}

	// Chat proxy enriches each /chat/{id} request with the conversation's
	// project_* fields (PROJ-09 layering) — requires projectService and
	// conversationRepo per Plan 15-04 Task 3.
	chatProxyHandler := handler.NewChatProxyHandler(
		businessService,
		integrationService,
		projectService,
		conversationRepo,
		messageRepo,
		pendingToolCallRepo,
		postRepo,
		reviewRepo,
		agentTaskRepo,
		taskHub,
		cfg.OrchestratorURL,
		nil,
		titler, // Phase 18 Plan 05 — optional auto-titler; nil when titling is disabled.
	)

	// Plan 16-07 HITL: resolve + resume + GET /tools handlers.
	// ToolsRegistryCache talks to the orchestrator's /internal/tools endpoint
	// with a 5-min TTL so settings/project pages + edit-validation share one
	// source of truth.
	toolsCache := service.NewToolsRegistryCache(cfg.OrchestratorURL, nil, 5*time.Minute)
	hitlService := service.NewHITLService(
		pendingToolCallRepo,
		businessRepo,
		projectRepo,
		toolsCache,
		cfg.OrchestratorURL,
		&http.Client{Timeout: 0}, // SSE requires no timeout
	)
	hitlHandler, err := handler.NewHITLHandler(hitlService, businessService, conversationRepo)
	if err != nil {
		return fmt.Errorf("create hitl handler: %w", err)
	}
	// Wire the shared ToolsRegistryCache into the business + project
	// handlers so PUT /business/{id}/tool-approvals and
	// PUT /projects/{id} can validate approval-overrides keys against the
	// live orchestrator registry before persisting (POLICY-05, POLICY-06).
	businessHandler.SetToolsCache(toolsCache)
	projectHandler.SetToolsCache(toolsCache)

	// Phase 19 Plan 19-03 — search handler. Constructed with the searcher
	// (built above; readiness flag already flipped) + businessService for
	// resolving the caller's businessID server-side from the bearer's
	// userID. Returns 503 + Retry-After: 5 only if MarkIndexesReady was
	// somehow not called — which cannot happen on the success path of
	// run() because EnsureSearchIndexes either returns nil and we flip
	// the flag, or returns an error and we abort startup.
	searchHandler, err := handler.NewSearchHandler(searcher, businessService)
	if err != nil {
		return fmt.Errorf("create search handler: %w", err)
	}

	handlers := &router.Handlers{
		Auth:          authHandler,
		Business:      businessHandler,
		Integration:   integrationHandler,
		Conversation:  conversationHandler,
		OAuth:         oauthHandler,
		InternalToken: internalTokenHandler,
		ChatProxy:     chatProxyHandler,
		Review:        reviewHandler,
		Post:          postHandler,
		AgentTask:     agentTaskHandler,
		Project:       projectHandler,
		HITL:          hitlHandler,
		Titler:        titlerHandler,
		Search:        searchHandler,
	}

	// Health checker
	hc := health.New()
	hc.AddCheck("postgres", func(ctx context.Context) error { return pgPool.Ping(ctx) })
	hc.AddCheck("redis", func(ctx context.Context) error { return redisClient.Ping(ctx).Err() })

	// Setup router
	r := router.Setup(handlers, []byte(cfg.JWTSecret), redisClient, hc)

	// Start HTTP server
	addr := ":" + cfg.Port
	srv := &http.Server{
		Addr:        addr,
		Handler:     r,
		ReadTimeout: 15 * time.Second,
		// WriteTimeout=0: SSE requires long-lived connections (/api/v1/chat/{id}
		// proxies the orchestrator stream, which may run for minutes while
		// RPA tool calls complete). Per-request deadlines are enforced by
		// context.WithTimeout in handlers that need them.
		WriteTimeout: 0,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	errCh := make(chan error, 1)
	go func() {
		log.Info("server listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	// Internal server
	internalRouter := router.SetupInternal(handlers, hc)
	internalAddr := ":" + cfg.InternalPort
	internalSrv := &http.Server{
		Addr:              internalAddr,
		Handler:           internalRouter,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		log.Info("internal server listening", "addr", internalAddr)
		if err := internalSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("internal server error", "error", err)
		}
	}()

	// Review syncer — optional, requires NATS_URL
	if cfg.NATSUrl != "" {
		nc, natsErr := natslib.Connect(cfg.NATSUrl)
		if natsErr != nil {
			log.Warn("NATS unavailable — review sync disabled", "url", cfg.NATSUrl, "error", natsErr)
		} else {
			defer nc.Close()
			syncInterval := time.Duration(cfg.ReviewSyncInterval) * time.Minute
			syncer := service.NewReviewSyncer(nc, integrationRepo, reviewRepo, syncInterval)
			syncCtx, syncCancel := context.WithCancel(ctx)
			defer syncCancel()
			go syncer.Start(syncCtx)
			log.Info("review syncer started", "interval_minutes", cfg.ReviewSyncInterval)
		}
	}

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-quit:
		log.Info("shutting down server")
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := internalSrv.Shutdown(shutdownCtx); err != nil {
		log.Error("internal server forced to shutdown", "error", err)
	}
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server forced to shutdown: %w", err)
	}

	log.Info("server stopped")
	return nil
}

// googleTokenRefresher implements service.TokenRefresher for Google OAuth2.
type googleTokenRefresher struct {
	clientID     string
	clientSecret string
	httpClient   *http.Client
}

func (r *googleTokenRefresher) RefreshToken(ctx context.Context, refreshToken string) (accessToken, newRefreshToken string, expiresIn int64, err error) {
	form := url.Values{
		"client_id":     {r.clientID},
		"client_secret": {r.clientSecret},
		"refresh_token": {refreshToken},
		"grant_type":    {"refresh_token"},
	}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://oauth2.googleapis.com/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", 0, fmt.Errorf("build refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return "", "", 0, fmt.Errorf("refresh request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", "", 0, fmt.Errorf("parse refresh response: %w", err)
	}
	if tokenResp.Error != "" {
		return "", "", 0, fmt.Errorf("google token refresh error: %s — %s", tokenResp.Error, tokenResp.ErrorDesc)
	}
	if tokenResp.AccessToken == "" {
		return "", "", 0, fmt.Errorf("google token refresh returned empty access token")
	}
	return tokenResp.AccessToken, tokenResp.RefreshToken, tokenResp.ExpiresIn, nil
}

// integrationSyncAdapter bridges service.IntegrationService to platform.integrationProvider.
type integrationSyncAdapter struct {
	svc service.IntegrationService
}

func (a *integrationSyncAdapter) ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]domain.Integration, error) {
	return a.svc.ListByBusinessID(ctx, businessID)
}

func (a *integrationSyncAdapter) GetDecryptedToken(ctx context.Context, businessID uuid.UUID, plt, externalID string) (string, error) {
	resp, err := a.svc.GetDecryptedToken(ctx, businessID, plt, externalID)
	if err != nil {
		return "", err
	}
	return resp.AccessToken, nil
}

// runToolApprovalStartupValidation implements POLICY-07 — compares every
// tool-approval entry stored in Postgres against the live orchestrator
// registry (fetched over HTTP) and logs tool_approval_whitelist_unknown for
// entries whose tool no longer exists. Unknown entries are NOT auto-pruned;
// they are treated as denied by the runtime policy resolver (Registry.Floor
// returns ToolFloorForbidden for unknown tools — enforced in Plan 16-03 Task 1).
//
// Non-blocking, best-effort: runs in a goroutine; one retry after 5s; skips
// silently on sustained failure so a slow/dead orchestrator cannot block API
// boot. The sweep is advisory — production alerts should watch for
// `tool_approval_whitelist_unknown` events in Loki/Grafana.
func runToolApprovalStartupValidation(_ context.Context, pgPool *pgxpool.Pool, orchestratorURL string) {
	sweepCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	registered, err := fetchOrchestratorToolNames(sweepCtx, orchestratorURL)
	if err != nil {
		slog.WarnContext(sweepCtx, "tool_approval_whitelist_sweep: fetch registry failed, retrying",
			"orchestrator", orchestratorURL, "error", err,
		)
		select {
		case <-time.After(5 * time.Second):
		case <-sweepCtx.Done():
			return
		}
		registered, err = fetchOrchestratorToolNames(sweepCtx, orchestratorURL)
		if err != nil {
			slog.WarnContext(sweepCtx, "tool_approval_whitelist_sweep: skipped (orchestrator unreachable)",
				"orchestrator", orchestratorURL, "error", err,
			)
			return
		}
	}

	businesses, err := loadBusinessApprovalSources(sweepCtx, pgPool)
	if err != nil {
		slog.ErrorContext(sweepCtx, "tool_approval_whitelist_sweep: failed to load businesses", "error", err)
		return
	}
	projects, err := loadProjectApprovalSources(sweepCtx, pgPool)
	if err != nil {
		slog.ErrorContext(sweepCtx, "tool_approval_whitelist_sweep: failed to load projects", "error", err)
		return
	}

	count := hitlvalidation.ValidateApprovalSettings(sweepCtx, registered, businesses, projects)
	slog.InfoContext(sweepCtx, "tool_approval_whitelist_unknown count",
		"count", count,
		"businesses_scanned", len(businesses),
		"projects_scanned", len(projects),
	)
}

// fetchOrchestratorToolNames calls GET {orchestratorURL}/internal/tools/names
// and decodes the `{names: [...]}` response into a map usable by
// hitlvalidation.ValidateApprovalSettings. A 10s timeout protects against
// a hung orchestrator; the caller handles retry.
func fetchOrchestratorToolNames(ctx context.Context, orchestratorURL string) (map[string]struct{}, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	u := strings.TrimRight(orchestratorURL, "/") + "/internal/tools/names"
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %s", resp.Status)
	}

	var body struct {
		Names []string `json:"names"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	out := make(map[string]struct{}, len(body.Names))
	for _, n := range body.Names {
		out[n] = struct{}{}
	}
	return out, nil
}

// loadBusinessApprovalSources reads every business's tool_approvals JSONB
// entry directly from Postgres. Materialized into the typed
// hitlvalidation.ApprovalSource shape so the validator stays decoupled from
// domain.Business. Skips businesses with no settings payload entirely.
func loadBusinessApprovalSources(ctx context.Context, pool *pgxpool.Pool) ([]hitlvalidation.ApprovalSource, error) {
	rows, err := pool.Query(ctx, "SELECT id, COALESCE(settings, '{}'::jsonb)::text FROM businesses")
	if err != nil {
		return nil, fmt.Errorf("query businesses: %w", err)
	}
	defer rows.Close()

	var out []hitlvalidation.ApprovalSource
	for rows.Next() {
		var (
			id       uuid.UUID
			settings string
		)
		if err := rows.Scan(&id, &settings); err != nil {
			return nil, fmt.Errorf("scan business row: %w", err)
		}
		overrides := extractToolApprovals(settings)
		if len(overrides) == 0 {
			continue
		}
		out = append(out, hitlvalidation.ApprovalSource{
			ID:        id.String(),
			Overrides: overrides,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate businesses: %w", err)
	}
	return out, nil
}

// loadProjectApprovalSources reads every project's approval_overrides JSONB
// column. Uses COALESCE so projects predating Phase 16 (null column) are
// surfaced as empty maps, not as an error.
func loadProjectApprovalSources(ctx context.Context, pool *pgxpool.Pool) ([]hitlvalidation.ApprovalSource, error) {
	rows, err := pool.Query(ctx, "SELECT id, COALESCE(approval_overrides, '{}'::jsonb)::text FROM projects")
	if err != nil {
		// Graceful degradation: if approval_overrides column doesn't yet exist
		// (migration ordering race in dev), skip projects rather than failing
		// the entire sweep.
		slog.WarnContext(ctx, "tool_approval_whitelist_sweep: projects query failed, skipping projects",
			"error", err,
		)
		return nil, nil
	}
	defer rows.Close()

	var out []hitlvalidation.ApprovalSource
	for rows.Next() {
		var (
			id        uuid.UUID
			overrides string
		)
		if err := rows.Scan(&id, &overrides); err != nil {
			return nil, fmt.Errorf("scan project row: %w", err)
		}
		parsed := parseToolFloorMap(overrides)
		if len(parsed) == 0 {
			continue
		}
		out = append(out, hitlvalidation.ApprovalSource{
			ID:        id.String(),
			Overrides: parsed,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate projects: %w", err)
	}
	return out, nil
}

// extractToolApprovals pulls the tool_approvals sub-object out of a
// businesses.settings JSONB payload. Returns an empty map if settings is
// malformed, missing, or if tool_approvals is absent. Any non-ToolFloor
// values are dropped silently (the startup sweep treats them as noise; the
// runtime resolver also ignores them via domain.Business.ToolApprovals()).
func extractToolApprovals(settingsJSON string) map[string]domain.ToolFloor {
	var outer map[string]interface{}
	if err := json.Unmarshal([]byte(settingsJSON), &outer); err != nil {
		return nil
	}
	raw, ok := outer["tool_approvals"].(map[string]interface{})
	if !ok {
		return nil
	}
	out := make(map[string]domain.ToolFloor, len(raw))
	for k, v := range raw {
		s, ok := v.(string)
		if !ok {
			continue
		}
		out[k] = domain.ToolFloor(s)
	}
	return out
}

// parseToolFloorMap decodes a JSONB string into a map[string]domain.ToolFloor.
// Invalid payloads yield nil — the sweep logs the issue indirectly because an
// empty overrides map produces zero warnings (the zero warnings is still
// safe behavior; a broken column would need a separate alert).
func parseToolFloorMap(s string) map[string]domain.ToolFloor {
	var raw map[string]string
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return nil
	}
	out := make(map[string]domain.ToolFloor, len(raw))
	for k, v := range raw {
		out[k] = domain.ToolFloor(v)
	}
	return out
}

// buildProviderOpts creates RouterOptions for every API key that is set in config,
// and registers the LLM model → provider mapping in the registry for each.
// Returns at least one option if any key is set, nil if none.
//
// Lifted verbatim from services/orchestrator/cmd/main.go:740-788 so the
// API-side titler Router constructs over byte-identical provider semantics
// (Phase 18 — Landmine 3). The only intentional difference between this
// copy and the orchestrator's is package locality; the body is unchanged
// so future audits can diff the two and confirm parity.
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
