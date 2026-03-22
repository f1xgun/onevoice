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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	natslib "github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/f1xgun/onevoice/pkg/crypto"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/health"
	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/services/api/internal/config"
	"github.com/f1xgun/onevoice/services/api/internal/handler"
	"github.com/f1xgun/onevoice/services/api/internal/platform"
	"github.com/f1xgun/onevoice/services/api/internal/repository"
	"github.com/f1xgun/onevoice/services/api/internal/router"
	"github.com/f1xgun/onevoice/services/api/internal/service"
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
	log.Info("connected to postgres")

	// MongoDB
	mongoClient, err := mongo.Connect(options.Client().ApplyURI(cfg.MongoURI))
	if err != nil {
		return fmt.Errorf("connect to mongodb: %w", err)
	}
	mongoDB := mongoClient.Database(cfg.MongoDB)
	log.Info("connected to mongodb")

	// Redis
	redisClient := redis.NewClient(&redis.Options{
		Addr: fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
	})
	defer func() { _ = redisClient.Close() }()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("connect to redis: %w", err)
	}
	log.Info("connected to redis")

	// Health checker
	hc := health.New()
	hc.AddCheck("postgres", func(ctx context.Context) error {
		return pgPool.Ping(ctx)
	})
	hc.AddCheck("mongodb", func(ctx context.Context) error {
		return mongoClient.Ping(ctx, nil)
	})
	hc.AddCheck("redis", func(ctx context.Context) error {
		return redisClient.Ping(ctx).Err()
	})

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

	// Initialize services
	userService, err := service.NewUserService(userRepo, redisClient, cfg.JWTSecret)
	if err != nil {
		return fmt.Errorf("init user service: %w", err)
	}
	businessService := service.NewBusinessService(businessRepo)
	integrationService := service.NewIntegrationService(integrationRepo, enc)
	oauthService := service.NewOAuthService(redisClient)
	reviewService := service.NewReviewService(reviewRepo, businessService)
	postService := service.NewPostService(postRepo, businessService)
	agentTaskService := service.NewAgentTaskService(agentTaskRepo, businessService)

	// Ensure upload directory exists
	if err := os.MkdirAll(cfg.UploadDir, 0o750); err != nil {
		return fmt.Errorf("create upload dir: %w", err)
	}

	// Platform syncer: pushes business info updates to connected platforms
	platformSyncer := platform.NewSyncer(
		&integrationSyncAdapter{svc: integrationService},
		nil,
		cfg.PublicURL,
	)
	platformSyncer.SetTaskRecorder(agentTaskRepo)

	// Initialize handlers
	oauthHandler := handler.NewOAuthHandler(oauthService, integrationService, businessService, handler.OAuthConfig{
		VKClientID:         cfg.VKClientID,
		VKClientSecret:     cfg.VKClientSecret,
		VKRedirectURI:      cfg.VKRedirectURI,
		YandexClientID:     cfg.YandexClientID,
		YandexClientSecret: cfg.YandexClientSecret,
		YandexRedirectURI:  cfg.YandexRedirectURI,
		TelegramBotToken:   cfg.TelegramBotToken,
	}, nil, redisClient)
	internalTokenHandler := handler.NewInternalTokenHandler(integrationService)
	chatProxyHandler := handler.NewChatProxyHandler(businessService, integrationService, messageRepo, postRepo, reviewRepo, agentTaskRepo, cfg.OrchestratorURL, nil)

	authHandler, err := handler.NewAuthHandler(userService, cfg.SecureCookies)
	if err != nil {
		return fmt.Errorf("init auth handler: %w", err)
	}
	businessHandler, err := handler.NewBusinessHandler(businessService, platformSyncer, cfg.UploadDir)
	if err != nil {
		return fmt.Errorf("init business handler: %w", err)
	}
	integrationHandler, err := handler.NewIntegrationHandler(integrationService, businessService)
	if err != nil {
		return fmt.Errorf("init integration handler: %w", err)
	}
	conversationHandler, err := handler.NewConversationHandler(conversationRepo, messageRepo)
	if err != nil {
		return fmt.Errorf("init conversation handler: %w", err)
	}
	reviewHandler, err := handler.NewReviewHandler(reviewService)
	if err != nil {
		return fmt.Errorf("init review handler: %w", err)
	}
	postHandler, err := handler.NewPostHandler(postService)
	if err != nil {
		return fmt.Errorf("init post handler: %w", err)
	}
	agentTaskHandler, err := handler.NewAgentTaskHandler(agentTaskService)
	if err != nil {
		return fmt.Errorf("init agent task handler: %w", err)
	}

	telemetryHandler := handler.NewTelemetryHandler()

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
		Telemetry:     telemetryHandler,
	}

	// Setup router
	r := router.Setup(handlers, []byte(cfg.JWTSecret), redisClient, cfg.UploadDir, hc)

	// Start HTTP server
	addr := ":" + cfg.Port
	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
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
	var nc *natslib.Conn
	if cfg.NATSUrl != "" {
		var natsErr error
		nc, natsErr = natslib.Connect(cfg.NATSUrl)
		if natsErr != nil {
			log.Warn("NATS unavailable — review sync disabled", "url", cfg.NATSUrl, "error", natsErr)
			nc = nil
		} else {
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

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	// 1. Stop HTTP servers
	if err := internalSrv.Shutdown(shutdownCtx); err != nil {
		log.Error("internal server forced to shutdown", "error", err)
	}
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("server forced to shutdown", "error", err)
	}

	// 2. Drain NATS if connected
	if nc != nil {
		_ = nc.Drain()
	}

	// 3. Close database pools
	pgPool.Close()
	if err := mongoClient.Disconnect(shutdownCtx); err != nil {
		log.Error("mongo disconnect error", "error", err)
	}

	log.Info("server stopped")
	return nil
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
