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

	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/f1xgun/onevoice/pkg/health"
	"github.com/f1xgun/onevoice/pkg/legalconfig"
	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/services/api/internal/config"
	"github.com/f1xgun/onevoice/services/api/internal/router"
	"github.com/f1xgun/onevoice/services/api/internal/service"
	"github.com/f1xgun/onevoice/services/api/internal/wire"
)

// shutdownTimeout bounds the graceful HTTP shutdown deadline. The HTTP
// read/idle/header timeouts are env-tunable (HTTP_READ_TIMEOUT etc.) per
// the env-config initiative — read off cfg.HTTPReadTimeout / etc. below.
// WriteTimeout=0 because /api/v1/chat/{id} proxies the orchestrator SSE
// stream, which may run for minutes while RPA tool calls complete;
// per-request deadlines are enforced inside handlers that need them.
const shutdownTimeout = 30 * time.Second

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

	entity := legalconfig.Load()
	if entity.IsPlaceholder() {
		if os.Getenv("LEGAL_ENFORCE") == "strict" {
			log.Error("legalconfig: LEGAL_* env vars still placeholder under LEGAL_ENFORCE=strict — refusing to start",
				"name_is_placeholder", entity.Name == legalconfig.PlaceholderName || entity.Name == "",
				"inn_empty", entity.INN == "",
				"address_empty", entity.Address == "",
				"email_pdn_is_placeholder", entity.EmailPDN == legalconfig.PlaceholderEmail || entity.EmailPDN == "",
			)
			return fmt.Errorf("legalconfig: production startup blocked — set LEGAL_ENTITY_NAME, LEGAL_INN, LEGAL_ADDRESS, LEGAL_EMAIL_PDN per docs/runbook-launch-readiness.md §6")
		}
		log.Warn("legalconfig: LEGAL_* env vars not fully configured — running with placeholder values (dev/staging only). Set LEGAL_ENFORCE=strict in production.")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	handles, err := wire.BootstrapDatabases(ctx, log, cfg)
	if err != nil {
		return err
	}
	defer handles.Close()

	repos := wire.Repositories(handles)
	svcs, err := wire.BuildServices(ctx, log, cfg, repos, handles)
	if err != nil {
		return err
	}
	defer svcs.Close()

	go wire.RunToolApprovalStartupValidation(ctx, handles.PG, svcs.OrchClient, cfg.OrchestratorFetchTimeout)

	wire.StartRetentionSweep(ctx, handles.PG, repos.AuditLog)
	wire.StartIntegrationsPurge(ctx, handles.PG, repos.Integration)

	emailSender, err := wire.BuildEmailSender(log, cfg)
	if err != nil {
		return err
	}
	wire.StartOutboxWorker(ctx, log, repos.EmailOutbox, emailSender, cfg.OutboxPollInterval, cfg.OutboxMaxAttempts)

	if svcs.AccountDeletion != nil {
		go runHardDeleteSweeper(ctx, log, svcs.AccountDeletion)
		go runDeletionWarningSweeper(ctx, log, svcs.AccountDeletion)
	}

	if svcs.BusinessDeletion != nil {
		go runBusinessHardDeleteSweeper(ctx, log, svcs.BusinessDeletion)
	}

	handlers, err := wire.Handlers(cfg, svcs, repos, handles)
	if err != nil {
		return err
	}

	hc := health.New(health.WithCheckTimeout(cfg.HealthCheckTimeout))
	var mongoClient *mongo.Client
	if handles.Mongo != nil {
		mongoClient = handles.Mongo.Client()
	}
	health.RegisterDefaultChecks(hc, handles.PG, mongoClient, handles.Redis, handles.NATS)

	return runServers(ctx, log, cfg, handlers, hc, svcs, handles, repos)
}

// runServers builds the public + internal chi routers and starts both
// http.Servers with graceful shutdown. Process-lifecycle code, not wiring —
// that is why it stays in cmd/main.go rather than internal/wire/.
func runServers(ctx context.Context, log *slog.Logger, cfg *config.Config, handlers *router.Handlers, hc *health.Checker, svcs *wire.Services, handles *wire.DBHandles, repos *wire.Repos) error {
	rateLimits := router.RateLimits{
		Register:    cfg.RateLimitRegister,
		Login:       cfg.RateLimitLogin,
		Chat:        cfg.RateLimitChat,
		HITL:        cfg.RateLimitHITL,
		Consents:    cfg.RateLimitConsents,
		Telemetry:   cfg.RateLimitTelemetry,
		Writes:      cfg.RateLimitWrites,
		Invitations: cfg.RateLimitInvitations,
	}
	r := router.Setup(handlers, []byte(cfg.JWTSecret), handles.Redis, hc, cfg.CORSAllowedOrigins, rateLimits, svcs.AuthzCache, repos.User, handles.PG, svcs.Lockout)

	addr := ":" + cfg.Port
	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  cfg.HTTPReadTimeout,
		WriteTimeout: 0,
		IdleTimeout:  cfg.HTTPIdleTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("server listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	internalRouter := router.SetupInternal(handlers, hc, cfg)
	internalAddr := ":" + cfg.InternalPort
	internalSrv := &http.Server{
		Addr:              internalAddr,
		Handler:           internalRouter,
		ReadHeaderTimeout: cfg.HTTPReadHeaderTimeout,
	}
	internalTLS, mtlsErr := wire.MaybeServerTLSConfig()
	if mtlsErr != nil {
		return fmt.Errorf("internal server tls: %w", mtlsErr)
	}
	if internalTLS != nil {
		internalSrv.TLSConfig = internalTLS
	}
	go func() {
		log.Info("internal server listening", "addr", internalAddr, "tls", internalTLS != nil)
		var serveErr error
		if internalTLS != nil {
			serveErr = internalSrv.ListenAndServeTLS("", "")
		} else {
			serveErr = internalSrv.ListenAndServe()
		}
		if serveErr != nil && serveErr != http.ErrServerClosed {
			log.Error("internal server error", "error", serveErr)
		}
	}()

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

// runHardDeleteSweeper —. Hourly cron entry
// that hard-deletes users whose deletion_requested_at < NOW - 30d.
// The 30-day grace is forgiving of an hour of cadence imprecision.
//
// Each batch is processed in its own service-level TX via FOR UPDATE
// SKIP LOCKED so concurrent CancelDeletion calls can race-win the row.
// Per-user errors are logged but do NOT abort the sweeper — the rest
// of the batch still attempts (T-DEL-12).
//
// Lifecycle bound to ctx (signal.NotifyContext-derived) so SIGTERM
// cancels the ticker cleanly.
func runHardDeleteSweeper(ctx context.Context, log *slog.Logger, svc *service.AccountDeletionService) {
	const tickInterval = 1 * time.Hour
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	log.InfoContext(ctx, "hard delete sweeper: starting", "interval", tickInterval.String())
	for {
		select {
		case <-ctx.Done():
			log.InfoContext(ctx, "hard delete sweeper: stopping")
			return
		case <-ticker.C:
			processed, err := svc.HardDeleteSweeper(ctx)
			if err != nil {
				log.WarnContext(ctx, "hard delete sweeper failed", "err", err)
				continue
			}
			if processed > 0 {
				log.InfoContext(ctx, "hard delete sweeper completed", "processed", processed)
			}
		}
	}
}

// runBusinessHardDeleteSweeper hard-deletes organizations whose
// deletion_requested_at < NOW - 30d. Hourly cadence (forgiving of an hour of
// imprecision against the 30-day grace). Each batch runs in its own service-level
// TX via FOR UPDATE SKIP LOCKED so concurrent CancelDeletion calls can race-win
// the row. Lifecycle bound to ctx so SIGTERM cancels the ticker cleanly.
func runBusinessHardDeleteSweeper(ctx context.Context, log *slog.Logger, svc *service.BusinessDeletionService) {
	const tickInterval = 1 * time.Hour
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	log.InfoContext(ctx, "business hard delete sweeper: starting", "interval", tickInterval.String())
	for {
		select {
		case <-ctx.Done():
			log.InfoContext(ctx, "business hard delete sweeper: stopping")
			return
		case <-ticker.C:
			processed, err := svc.HardDeleteSweeper(ctx)
			if err != nil {
				log.WarnContext(ctx, "business hard delete sweeper failed", "err", err)
				continue
			}
			if processed > 0 {
				log.InfoContext(ctx, "business hard delete sweeper completed", "processed", processed)
			}
		}
	}
}

// runDeletionWarningSweeper —. Every 6h
// it scans the T-7 window (22d23h..23d ago) and enqueues a warning
// email per user, deduped via ExistsBySubjectAndRecipient. The 1h-wide
// sweep window with 6h cadence guarantees coverage — no T-7 email
// missed even with a 5h sweeper outage (T-DEL-08).
//
// This sweeper is the safety net for the request-time deferred enqueue
// (RequestDeletion calls EnqueueDeferred at +23d). If that deferred
// row is missing (e.g. lost across a cancel→re-request churn), this
// sweeper recovers within 6 hours.
func runDeletionWarningSweeper(ctx context.Context, log *slog.Logger, svc *service.AccountDeletionService) {
	const tickInterval = 6 * time.Hour
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	log.InfoContext(ctx, "deletion warning sweeper: starting", "interval", tickInterval.String())
	for {
		select {
		case <-ctx.Done():
			log.InfoContext(ctx, "deletion warning sweeper: stopping")
			return
		case <-ticker.C:
			enqueued, err := svc.WarningSweeper(ctx)
			if err != nil {
				log.WarnContext(ctx, "deletion warning sweeper failed", "err", err)
				continue
			}
			if enqueued > 0 {
				log.InfoContext(ctx, "deletion warning sweeper enqueued T-7 mails", "count", enqueued)
			}
		}
	}
}
