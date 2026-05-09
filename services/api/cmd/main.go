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

	"github.com/f1xgun/onevoice/pkg/health"
	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/services/api/internal/config"
	"github.com/f1xgun/onevoice/services/api/internal/router"
	"github.com/f1xgun/onevoice/services/api/internal/wire"
)

// HTTP server lifecycle timeouts. WriteTimeout=0 because /api/v1/chat/{id}
// proxies the orchestrator SSE stream, which may run for minutes while RPA
// tool calls complete; per-request deadlines are enforced inside handlers
// that need them. ReadHeaderTimeout on the internal listener guards against
// slow-loris on the metrics endpoint.
const (
	httpReadTimeout       = 15 * time.Second
	httpReadHeaderTimeout = 10 * time.Second
	httpIdleTimeout       = 60 * time.Second
	shutdownTimeout       = 30 * time.Second
)

func main() {
	log := logger.New("api")
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
	log.Info("starting onevoice api server")

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	handles, err := wire.BootstrapDatabases(ctx, log, cfg)
	if err != nil {
		return err
	}
	defer handles.Close()

	// POLICY-07 startup sweep — non-blocking goroutine. Compares every
	// tool-approval entry stored in Postgres against the live orchestrator
	// registry and logs tool_approval_whitelist_unknown for stale entries.
	// Best-effort: one retry after 5s, skipped silently on sustained failure.
	go wire.RunToolApprovalStartupValidation(ctx, handles.PG, cfg.OrchestratorURL)

	repos := wire.Repositories(handles)
	svcs, err := wire.BuildServices(ctx, log, cfg, repos, handles)
	if err != nil {
		return err
	}
	defer svcs.Close()

	handlers, err := wire.Handlers(cfg, svcs, repos, handles)
	if err != nil {
		return err
	}

	hc := health.New()
	hc.AddCheck("postgres", func(ctx context.Context) error { return handles.PG.Ping(ctx) })
	hc.AddCheck("redis", func(ctx context.Context) error { return handles.Redis.Ping(ctx).Err() })

	return runServers(ctx, log, cfg, handlers, hc, svcs, handles)
}

// runServers builds the public + internal chi routers and starts both
// http.Servers with graceful shutdown. Process-lifecycle code, not wiring —
// that is why it stays in cmd/main.go rather than internal/wire/.
func runServers(ctx context.Context, log *slog.Logger, cfg *config.Config, handlers *router.Handlers, hc *health.Checker, svcs *wire.Services, handles *wire.DBHandles) error {
	r := router.Setup(handlers, []byte(cfg.JWTSecret), handles.Redis, hc)

	addr := ":" + cfg.Port
	srv := &http.Server{
		Addr:        addr,
		Handler:     r,
		ReadTimeout: httpReadTimeout,
		// WriteTimeout=0: SSE requires long-lived connections.
		WriteTimeout: 0,
		IdleTimeout:  httpIdleTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("server listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	internalRouter := router.SetupInternal(handlers, hc)
	internalAddr := ":" + cfg.InternalPort
	internalSrv := &http.Server{
		Addr:              internalAddr,
		Handler:           internalRouter,
		ReadHeaderTimeout: httpReadHeaderTimeout,
	}
	go func() {
		log.Info("internal server listening", "addr", internalAddr)
		if err := internalSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("internal server error", "error", err)
		}
	}()

	// Review syncer ticker — start the background pull loop using the
	// reviewSyncer instance built earlier (so reviewService can share it
	// for the manual-refresh endpoint). No-op when nil.
	svcs.StartReviewSyncer(ctx, log, cfg.ReviewSyncInterval)

	select {
	case <-ctx.Done():
		log.Info("shutting down server")
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	}

	shutdownCtx, shutCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutCancel()

	if err := internalSrv.Shutdown(shutdownCtx); err != nil {
		log.Error("internal server forced to shutdown", "error", err)
	}
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server forced to shutdown: %w", err)
	}

	log.Info("server stopped")
	return nil
}
