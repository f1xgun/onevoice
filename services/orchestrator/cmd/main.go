// Package main is the orchestrator entry point. Wiring lives in
// internal/wire/; this file only owns process lifecycle: signal handling,
// chi-router middleware chain, http.Server start, graceful drain. The
// budget caps this file at 200 LOC — anything longer than the
// run/runServers pair below belongs in wire/.
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

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"

	"github.com/f1xgun/onevoice/pkg/billingclient"
	"github.com/f1xgun/onevoice/pkg/health"
	"github.com/f1xgun/onevoice/pkg/i18n"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/pkg/metrics"
	"github.com/f1xgun/onevoice/pkg/mtls"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/config"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/orchestrator"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/wire"
)

const (
	mongoShutdownTimeout = 5 * time.Second
	httpReadTimeout      = 15 * time.Second
	redisPingTimeout     = 3 * time.Second
)

// newRedisClient builds a *redis.Client from a connection string and verifies
// connectivity with a short Ping. Returns an error on dial / auth failure so
// the orchestrator fails to boot rather than handing the rate limiter a
// half-broken client.
func newRedisClient(ctx context.Context, url string) (*redis.Client, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse REDIS_URL: %w", err)
	}
	rdb := redis.NewClient(opts)
	pingCtx, cancel := context.WithTimeout(ctx, redisPingTimeout)
	defer cancel()
	if perr := rdb.Ping(pingCtx).Err(); perr != nil {
		return nil, fmt.Errorf("redis ping: %w", perr)
	}
	return rdb, nil
}

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
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log.Info("mtls", "enabled", mtls.IsEnabled())

	billingHTTP, err := billingclient.New(cfg.APIInternalURL, nil)
	if err != nil {
		return fmt.Errorf("billingclient init: %w", err)
	}
	log.Info("billing client wired", "url", cfg.APIInternalURL)

	routerOpts := []llm.RouterOption{llm.WithBilling(billingHTTP)}
	if cfg.RedisURL != "" {
		rdb, redisErr := newRedisClient(ctx, cfg.RedisURL)
		if redisErr != nil {
			return fmt.Errorf("redis: %w", redisErr)
		}
		rl, rlErr := wire.BuildRateLimiter(cfg, log, rdb, billingHTTP)
		if rlErr != nil {
			return fmt.Errorf("rate limiter: %w", rlErr)
		}
		routerOpts = append(routerOpts, llm.WithRateLimiter(rl))
		log.Info("rate limiter wired",
			"policy", cfg.RedisDownPolicy,
			"free_tier_daily_spend_usd", cfg.FreeTierDailySpendUSD,
		)
	} else {
		log.Warn("rate limiter disabled: REDIS_URL is unset — cost guards inactive")
	}

	router, err := wire.LLMRouter(cfg, log, routerOpts...)
	if err != nil {
		return err
	}

	mongoDB, pendingRepo, err := wire.Mongo(ctx, log, cfg)
	if err != nil {
		return err
	}
	defer func() {
		shutCtx, shutCancel := context.WithTimeout(context.Background(), mongoShutdownTimeout)
		defer shutCancel()
		_ = mongoDB.Client().Disconnect(shutCtx)
	}()

	registry, nc, err := wire.Tools(log, cfg)
	if err != nil {
		return err
	}
	defer func() {
		if nc != nil {
			_ = nc.Drain()
		}
	}()

	hc := health.New(health.WithCheckTimeout(cfg.HealthCheckTimeout))
	health.RegisterDefaultChecks(hc, nil, mongoDB.Client(), nil, nc)

	orch := orchestrator.NewWithHITL(router, registry, pendingRepo, orchestrator.Options{
		MaxIterations:         cfg.MaxIterations,
		ConversationInputCap:  cfg.ConversationInputCap,
		ConversationOutputCap: cfg.ConversationOutputCap,
		RedactOutboundPDn:     !cfg.AllowTransborderLLM,
	})
	handlers := wire.Handlers(orch, registry, router, cfg)

	return runServers(ctx, log, cfg, handlers, hc)
}

// runServers builds the chi router (middleware + routes), starts the
// http.Server in a goroutine, and waits for either a fatal listen error or
// signal-driven shutdown. Mirrors the historical lifecycle block at
// services/orchestrator/cmd/main.go:157-225 — same middleware order, same
// route paths, same drain timeout. Process-lifecycle code lives here, not
// in wire/.
func runServers(ctx context.Context, log *slog.Logger, cfg *config.Config, h *wire.HandlerSet, hc *health.Checker) error {
	r := chi.NewRouter()
	r.Use(chimiddleware.RequestID)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			rctx := req.Context()
			if corrID := req.Header.Get("X-Correlation-ID"); corrID != "" {
				rctx = logger.WithCorrelationID(rctx, corrID)
			}
			next.ServeHTTP(w, req.WithContext(rctx))
		})
	})
	r.Use(i18n.LocaleMiddleware)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(metrics.HTTPMiddleware)

	r.Post("/chat/{conversationID}", h.Chat.Chat)
	r.Post("/chat/{conversationID}/resume", h.Resume.Resume)
	r.Get("/internal/tools/names", h.Tools.Names)
	r.Get("/internal/tools", h.ToolsAll.ServeHTTP)
	r.Post("/internal/draft-reply", h.DraftReply.ServeHTTP)
	r.Handle("/metrics", promhttp.Handler())
	r.Get("/health/live", hc.LiveHandler())
	r.Get("/health/ready", hc.ReadyHandler())
	r.Get("/health", hc.LiveHandler())

	addr := ":" + cfg.Port
	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  httpReadTimeout,
		WriteTimeout: 0,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("orchestrator listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		log.Info("shutting down orchestrator")
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	}

	shutCtx, shutCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer shutCancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		log.Error("http shutdown error", "error", err)
	}
	log.Info("orchestrator stopped")
	return nil
}
