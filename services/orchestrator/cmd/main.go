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
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Surface the mTLS posture at startup so a misconfigured deploy is
	// visible in the first log line rather than during the first internal
	// HTTP call. tokenclient + billingclient pick up the same env.
	log.Info("mtls", "enabled", mtls.IsEnabled())

	// Plan 25a-05: wire pkg/billingclient against the api service's mTLS
	// internal :8443 listener. Passing nil http.Client makes billingclient's
	// default transport honor ONEVOICE_MTLS_* env (same shape as tokenclient
	// from 25a-01), so no per-service drift. WithBilling threads it through
	// to llm.Router.logBilling — every successful Chat() call with a non-Nil
	// BusinessID now persists a usage_logs row.
	billingHTTP := billingclient.New(cfg.APIInternalURL, nil)
	log.Info("billing client wired", "url", cfg.APIInternalURL)

	router, err := wire.LLMRouter(cfg, log, llm.WithBilling(billingHTTP))
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

	// Same source-of-truth helper as services/api. Orchestrator owns
	// Mongo + (optional) NATS only — pass nil for PG and Redis so the
	// helper silently skips them. WithCheckTimeout wires the env-driven knob.
	hc := health.New(health.WithCheckTimeout(cfg.HealthCheckTimeout))
	health.RegisterDefaultChecks(hc, nil, mongoDB.Client(), nil, nc)

	// pendingRepo wires HITL pause-time persistence so manual-floor
	// tool calls can be saved as PendingToolCallBatch documents. Without it,
	// stepRun emits EventError "HITL not configured".
	orch := orchestrator.NewWithHITL(router, registry, pendingRepo, orchestrator.Options{
		MaxIterations: cfg.MaxIterations,
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
	// LocaleResolver runs after correlation but before logger/recoverer so
	// the resolved language.Tag is available to every downstream handler
	// (chat / draft-reply / tool list) for prompt-builder localization.
	// See pkg/i18n + Phase A1 of `.planning/i18n-readiness/PLAN.md`.
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
	r.Get("/health", hc.LiveHandler()) // backward compat

	addr := ":" + cfg.Port
	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  httpReadTimeout,
		WriteTimeout: 0, // SSE requires long-lived connections
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
