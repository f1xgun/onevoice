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

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/f1xgun/onevoice/pkg/crypto"
	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/services/api/internal/config"
	"github.com/f1xgun/onevoice/services/api/internal/handler"
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
	reviewRepo := repository.NewReviewRepository(mongoDB)
	postRepo := repository.NewPostRepository(mongoDB)
	agentTaskRepo := repository.NewAgentTaskRepository(mongoDB)

	// Initialize services
	userService := service.NewUserService(userRepo, redisClient, cfg.JWTSecret)
	businessService := service.NewBusinessService(businessRepo)
	integrationService := service.NewIntegrationService(integrationRepo, enc)
	oauthService := service.NewOAuthService(redisClient)
	reviewService := service.NewReviewService(reviewRepo, businessService)
	postService := service.NewPostService(postRepo, businessService)
	agentTaskService := service.NewAgentTaskService(agentTaskRepo, businessService)

	// Initialize handlers
	oauthHandler := handler.NewOAuthHandler(oauthService, integrationService, businessService, handler.OAuthConfig{
		VKClientID:         cfg.VKClientID,
		VKClientSecret:     cfg.VKClientSecret,
		VKRedirectURI:      cfg.VKRedirectURI,
		YandexClientID:     cfg.YandexClientID,
		YandexClientSecret: cfg.YandexClientSecret,
		YandexRedirectURI:  cfg.YandexRedirectURI,
		TelegramBotToken:   cfg.TelegramBotToken,
	}, nil)
	internalTokenHandler := handler.NewInternalTokenHandler(integrationService)
	chatProxyHandler := handler.NewChatProxyHandler(businessService, integrationService, cfg.OrchestratorURL, nil)

	handlers := &router.Handlers{
		Auth:          handler.NewAuthHandler(userService),
		Business:      handler.NewBusinessHandler(businessService),
		Integration:   handler.NewIntegrationHandler(integrationService, businessService),
		Conversation:  handler.NewConversationHandler(conversationRepo),
		OAuth:         oauthHandler,
		InternalToken: internalTokenHandler,
		ChatProxy:     chatProxyHandler,
		Review:        handler.NewReviewHandler(reviewService),
		Post:          handler.NewPostHandler(postService),
		AgentTask:     handler.NewAgentTaskHandler(agentTaskService),
	}

	// Setup router
	r := router.Setup(handlers, []byte(cfg.JWTSecret), redisClient)

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
	internalRouter := router.SetupInternal(handlers)
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
