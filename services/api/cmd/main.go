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
	"github.com/f1xgun/onevoice/pkg/metrics"
	"github.com/f1xgun/onevoice/services/api/internal/config"
	"github.com/f1xgun/onevoice/services/api/internal/router"
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
		metrics.MarkSweeperSuccess(metrics.SweeperAccountHardDelete)
		metrics.MarkSweeperSuccess(metrics.SweeperDeletionWarning)
		svcs.AccountDeletion.SetWarnScanWindow(deletionWarningTick)
		go runSweeper(ctx, log, metrics.SweeperAccountHardDelete, accountHardDeleteTick, svcs.AccountDeletion.HardDeleteSweeper)
		go runSweeper(ctx, log, metrics.SweeperDeletionWarning, deletionWarningTick, svcs.AccountDeletion.WarningSweeper)
	}

	if svcs.BusinessDeletion != nil {
		metrics.MarkSweeperSuccess(metrics.SweeperBusinessHardDelete)
		go runSweeper(ctx, log, metrics.SweeperBusinessHardDelete, businessHardDeleteTick, svcs.BusinessDeletion.HardDeleteSweeper)
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
	if err := cfg.RequireInternalMTLS(internalTLS != nil); err != nil {
		return err
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

// Sweeper cadences. The hard-delete sweepers run hourly — the 30-day grace
// before a user/org row is purged is forgiving of an hour of imprecision. The
// deletion-warning sweeper runs every 6h; its scan window is tied to this same
// tick (via SetWarnScanWindow) so the window is never narrower than the
// cadence — otherwise a user whose T-7 moment fell between two ticks would
// never be enumerated and never warned. Each sweep is idempotent (hard-deletes
// use FOR UPDATE SKIP LOCKED; warnings dedupe via ExistsBySubjectAndRecipient).
const (
	accountHardDeleteTick  = 1 * time.Hour
	businessHardDeleteTick = 1 * time.Hour
	deletionWarningTick    = 6 * time.Hour
)

// sweeperFunc runs one sweep pass and returns the number of items acted upon.
// AccountDeletionService.HardDeleteSweeper/WarningSweeper and
// BusinessDeletionService.HardDeleteSweeper all satisfy it.
type sweeperFunc func(ctx context.Context) (int, error)

// runSweeper drives a background sweeper: one pass per tick, each observed via
// the sweeper_* metrics so a wedged compliance-critical job is alertable
// (sweeper_last_success_timestamp ages past its staleness threshold). Per-pass
// errors are logged and metric'd but never abort the loop — the next tick
// retries. Lifecycle is bound to ctx so SIGTERM cancels the ticker cleanly.
func runSweeper(ctx context.Context, log *slog.Logger, name string, interval time.Duration, fn sweeperFunc) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	log.InfoContext(ctx, "sweeper starting", "sweeper", name, "interval", interval.String())
	for {
		select {
		case <-ctx.Done():
			log.InfoContext(ctx, "sweeper stopping", "sweeper", name)
			return
		case <-ticker.C:
			n, err := fn(ctx)
			if err != nil {
				metrics.IncSweeperRun(name, metrics.SweeperResultError)
				log.WarnContext(ctx, "sweeper failed", "sweeper", name, "err", err)
				continue
			}
			metrics.IncSweeperRun(name, metrics.SweeperResultOK)
			metrics.MarkSweeperSuccess(name)
			if n > 0 {
				metrics.AddSweeperItems(name, n)
				log.InfoContext(ctx, "sweeper completed", "sweeper", name, "processed", n)
			}
		}
	}
}
