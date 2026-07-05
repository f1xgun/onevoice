// Package wire constructs the API service's dependency graph at startup.
//
// See docs/api/wire-services.md for the services DI graph + construction
// order, and docs/api/wire-handlers.md for the handlers DI graph.
package wire

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/legalconfig"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/lockout"
	"github.com/f1xgun/onevoice/pkg/metrics"
	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
	"github.com/f1xgun/onevoice/services/api/internal/config"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/platform"
	"github.com/f1xgun/onevoice/services/api/internal/service"
	"github.com/f1xgun/onevoice/services/api/internal/service/planresolver"
	"github.com/f1xgun/onevoice/services/api/internal/storage"
	"github.com/f1xgun/onevoice/services/api/internal/taskhub"
)

// Services aggregates every business-logic service the API consumes.
type Services struct {
	User          service.UserService
	Business      service.BusinessService
	Integration   service.IntegrationService
	OAuth         *service.OAuthService
	Post          service.PostService
	Review        service.ReviewService
	AgentTask     service.AgentTaskService
	Project       *service.ProjectService
	Conversation  *service.ConversationService
	HITL          *service.HITLService
	Titler        *service.Titler // may be nil — graceful disable
	Searcher      *service.Searcher
	ToolsCache    *service.ToolsRegistryCache
	PlatformSync  *platform.Syncer
	ReviewSyncer  *service.ReviewSyncer
	Reconciler    *service.ReconciliationService
	TaskHub       *taskhub.Hub
	ObjectStorage *storage.MinioClient

	// OrchClient is the shared orchestrator HTTP client (chat / resume /
	// internal tool registry). Timeout=0 — per-call ctx bounds the budget so
	// SSE streams are not killed mid-flight.
	OrchClient *orchestratorclient.Client

	// AgentTaskPublisher is exposed so OAuthHandler can WithAgentTaskPublisher
	// when NATS is available. nil when NATS is unreachable.
	AgentTaskPublisher *platform.NATSTaskPublisher

	// AuthzCache backs RequireBusinessAccess. Non-nil — middleware panics on
	// nil cache at request time.
	AuthzCache *authz.Cache

	// AuditLogger is the fire-and-forget async audit writer.
	AuditLogger audit.Logger

	// PasswordReset is injected into AuthHandler via SetPasswordResetService.
	PasswordReset *service.PasswordResetService

	// EmailVerification is injected into AuthHandler via
	// SetEmailVerificationService AND into UserService.Register via
	// IssueAndEnqueueTx — token + outbox must commit in the same tx as the
	// user row + user_consents INSERT.
	EmailVerification *service.EmailVerificationService

	// AccountDeletion is wired into UserDeletionHandler + into cmd/main.go's
	// runHardDeleteSweeper / runDeletionWarningSweeper goroutines.
	AccountDeletion *service.AccountDeletionService

	// BusinessDeletion is wired into BusinessDeletionHandler + into
	// cmd/main.go's runBusinessHardDeleteSweeper goroutine.
	BusinessDeletion *service.BusinessDeletionService

	// Consent is wired into ConsentsHandler + into UserService.Register via
	// SetRegisterConsentService so the 3-row UPSERT runs in the same tx as
	// the user row.
	Consent *service.ConsentService

	// Telemetry persists frontend product-analytics events.
	Telemetry *service.TelemetryService

	// Feedback persists in-app user feedback + founder notification.
	Feedback *service.FeedbackService

	// PlanResolver resolves the per-business billing plan + rate-limit tier
	// (v1.6). Fail-safe: DB error / no subscription → Free. Injected into the
	// chat turn (tier forwarding) and the billing summary service.
	PlanResolver *planresolver.Resolver

	// Lockout is non-nil whenever h.Redis is non-nil (Redis is the storage
	// layer). SmartCaptcha is always non-nil — Noop impl when
	// SMARTCAPTCHA_SECRET_KEY is empty so the handler has a stable
	// dependency to inject.
	Lockout      *lockout.Lockout
	SmartCaptcha service.SmartCaptchaVerifier

	// reviewSyncerCancel stops the background ticker. nil when ReviewSyncer
	// is nil.
	reviewSyncerCancel context.CancelFunc

	// reconcilerCancel stops the proactive-sync reconciler loop. nil when the
	// reconciler is not started (SYNC_RECONCILE_ENABLED=false).
	reconcilerCancel context.CancelFunc
}

// Close stops background goroutines (review syncer ticker). Safe to call
// multiple times. NATS connection lifecycle stays with DBHandles.
func (s *Services) Close() {
	if s == nil {
		return
	}
	if s.reviewSyncerCancel != nil {
		s.reviewSyncerCancel()
	}
	if s.reconcilerCancel != nil {
		s.reconcilerCancel()
	}
}

// StartReviewSyncer starts the background review-syncer ticker. Idempotent
// no-op when the syncer is nil. The ticker stops when either the parent ctx
// cancels or Services.Close is called.
//
// The goroutine is registered on wg (wg.Add before the spawn, wg.Done on
// return) so the shutdown sequence joins an in-flight sync pass before the
// database pools close — without enrollment the syncer would race against
// mongoClient.Disconnect / NATS.Close / PG.Close and write to a closed pool.
func (s *Services) StartReviewSyncer(ctx context.Context, wg *sync.WaitGroup, log *slog.Logger, intervalMinutes int) {
	if s == nil || s.ReviewSyncer == nil {
		return
	}
	syncCtx, syncCancel := context.WithCancel(ctx)
	s.reviewSyncerCancel = syncCancel
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.ReviewSyncer.Start(syncCtx)
	}()
	log.Info("review syncer started", "interval_minutes", intervalMinutes)
}

// StartReconciler starts the proactive platform-sync reconciler loop. It is a
// no-op when the reconciler is nil OR enabled is false — the reconciler SHIPS
// DARK, so the default deploy carries zero extra polling load while the
// sync_state table and the drift/verify endpoints still exist.
//
// The goroutine is enrolled on wg (wg.Add before the spawn, wg.Done on return)
// so shutdown joins an in-flight reconcile pass before the database pools close,
// exactly like StartReviewSyncer. The loop stops when the parent ctx cancels or
// Services.Close is called.
func (s *Services) StartReconciler(ctx context.Context, wg *sync.WaitGroup, log *slog.Logger, enabled bool, pollInterval time.Duration) {
	if s == nil || s.Reconciler == nil {
		return
	}
	if !enabled {
		log.Info("sync reconciler disabled (SYNC_RECONCILE_ENABLED=false)")
		return
	}
	metrics.MarkSweeperSuccess(metrics.SweeperSyncReconcile)
	syncCtx, syncCancel := context.WithCancel(ctx)
	s.reconcilerCancel = syncCancel
	wg.Add(1)
	go func() {
		defer wg.Done()
		runReconcileLoop(syncCtx, log, s.Reconciler.Reconcile, pollInterval)
	}()
	log.Info("sync reconciler started", "poll_interval", pollInterval.String())
}

// runReconcileLoop drives the reconciler: one pass per tick, each observed via
// the sweeper_* metrics (so a wedged reconcile is alertable via
// sweeper_last_success_timestamp) — the same heartbeat idiom as the compliance
// sweepers in cmd/main.go. Per-pass errors are logged + metric'd but never abort
// the loop. Bound to ctx so SIGTERM / Close cancels the ticker cleanly.
func runReconcileLoop(ctx context.Context, log *slog.Logger, fn func(context.Context) (int, error), interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := fn(ctx)
			if err != nil {
				metrics.IncSweeperRun(metrics.SweeperSyncReconcile, metrics.SweeperResultError)
				log.WarnContext(ctx, "sync reconcile pass failed", "err", err)
				continue
			}
			metrics.IncSweeperRun(metrics.SweeperSyncReconcile, metrics.SweeperResultOK)
			metrics.MarkSweeperSuccess(metrics.SweeperSyncReconcile)
			if n > 0 {
				metrics.AddSweeperItems(metrics.SweeperSyncReconcile, n)
				log.InfoContext(ctx, "sync reconcile: channels drifting", "count", n)
			}
		}
	}
}

// BuildServices constructs the business-logic layer. See
// docs/api/wire-services.md for the ordering invariants.
//
// Named BuildServices (not Services) because Go forbids a type and a
// function with identical identifiers in one package — Services is the
// returned aggregate type.
func BuildServices(ctx context.Context, log *slog.Logger, cfg *config.Config, repos *Repos, h *DBHandles) (*Services, error) {
	orchClient := orchestratorclient.New(cfg.OrchestratorURL, &http.Client{Timeout: 0})

	s := &Services{
		TaskHub:     taskhub.New(),
		OrchClient:  orchClient,
		AuditLogger: audit.NewLoggerWithResolver(repos.AuditLog, userResolverAdapter{repo: repos.User}),
	}

	s.PlanResolver = planresolver.New(
		planresolver.NewRepoStore(repos.Subscription, repos.PlanDefinition),
		planresolver.DefaultTTL,
	)

	titlerModel := cfg.TitlerModel
	llmRouter, err := buildTitlerRouter(cfg, log, h.Redis, repos.Billing)
	if err != nil {
		return nil, err
	}
	if llmRouter != nil {
		s.Titler = service.NewTitler(llmRouter, repos.Conversation, titlerModel)
		log.Info("auto-titler: service constructed", "model", titlerModel)
	}

	s.Searcher = service.NewSearcher(repos.Conversation, repos.Message)
	s.Searcher.MarkIndexesReady()

	userService, err := service.NewUserService(repos.User, h.Redis, cfg.JWTSecret)
	if err != nil {
		return nil, fmt.Errorf("wire: create user service: %w", err)
	}
	s.User = userService
	s.Business = service.NewBusinessService(repos.Business, repos.BusinessMembership, repos.Role, h.PG, s.AuditLogger)

	s.AuthzCache = authz.NewCache(repos.MembershipLoader)

	var refresher service.TokenRefresher
	if cfg.GoogleClientID != "" && cfg.GoogleClientSecret != "" {
		refresher = NewGoogleTokenRefresher(
			cfg.GoogleClientID,
			cfg.GoogleClientSecret,
			&http.Client{Timeout: cfg.OrchestratorFetchTimeout},
		)
	}
	s.Integration = service.NewIntegrationService(repos.Integration, h.Envelope, h.PG, refresher, s.AuditLogger)
	s.Integration = service.WithActorGate(s.Integration, repos.User)
	s.Integration = service.WithMembershipGate(s.Integration, s.AuthzCache)
	s.Integration = service.WithBusinessGate(s.Integration, repos.Business)
	if h.NATS != nil {
		s.Integration = service.WithNATSPublisher(s.Integration, h.NATS)
	}
	s.OAuth = service.NewOAuthService(h.Redis)
	s.Post = service.NewPostService(repos.Post, s.Business)
	s.AgentTask = service.NewAgentTaskService(repos.AgentTask, s.Business, h.NATS)
	s.Project = service.NewProjectService(repos.Project, s.AuditLogger)

	conversationService, err := service.NewConversationService(repos.Conversation, repos.Message, repos.Project, h.PendingToolCallRepo)
	if err != nil {
		return nil, fmt.Errorf("wire: create conversation service: %w", err)
	}
	s.Conversation = conversationService

	objectStorage, err := storage.NewMinioClient(ctx, storage.Config{
		Endpoint:        cfg.S3Endpoint,
		AccessKey:       cfg.S3AccessKey,
		SecretKey:       cfg.S3SecretKey,
		Bucket:          cfg.S3Bucket,
		UseSSL:          cfg.S3UseSSL,
		PublicURLPrefix: cfg.S3PublicURLPrefix,
	})
	if err != nil {
		return nil, fmt.Errorf("wire: init object storage: %w", err)
	}
	s.ObjectStorage = objectStorage
	log.Info("connected to object storage", "endpoint", cfg.S3Endpoint, "bucket", cfg.S3Bucket)

	if h.NATS != nil {
		var drafter service.DraftPassRunner
		if cfg.ReviewDraftEnabled {
			drafter = service.NewReviewDrafter(
				repos.Review,
				repos.Business,
				orchClient,
				cfg.ReviewDraftMaxExamples,
				cfg.ReviewDraftBatchLimit,
			)
			log.Info("review AI-drafter enabled",
				"orchestrator_url", cfg.OrchestratorURL,
				"max_examples", cfg.ReviewDraftMaxExamples,
				"batch_limit", cfg.ReviewDraftBatchLimit,
			)
		}
		syncInterval := time.Duration(cfg.ReviewSyncInterval) * time.Minute
		s.ReviewSyncer = service.NewReviewSyncer(h.NATS, repos.Integration, repos.Review, drafter, syncInterval)
	}

	var reviewRefresher service.ReviewRefresher
	if s.ReviewSyncer != nil {
		reviewRefresher = s.ReviewSyncer
	}
	s.Review = service.NewReviewService(repos.Review, s.Business, h.NATS, reviewRefresher)

	adapter := IntegrationSyncAdapter(s.Integration)
	platformHTTPClient := &http.Client{Timeout: 10 * time.Second}
	if h.NATS != nil {
		s.AgentTaskPublisher = platform.NewNATSTaskPublisher(h.NATS)
	}
	var yandexPublisher platform.TaskPublisher
	if s.AgentTaskPublisher != nil {
		yandexPublisher = s.AgentTaskPublisher
	}
	telegramSyncer := platform.NewTelegramSyncer(adapter, platformHTTPClient, "", cfg.PublicURL)
	vkSyncer := platform.NewVKSyncer(adapter, platformHTTPClient, "")
	perPlatform := map[string]any{
		a2a.AgentTelegram:       telegramSyncer,
		a2a.AgentVK:             vkSyncer,
		a2a.AgentYandexBusiness: platform.NewYandexSyncer(yandexPublisher),
	}
	s.PlatformSync = platform.NewSyncer(adapter, repos.AgentTask, s.TaskHub, perPlatform)

	// Proactive platform-sync reconciler (ships DARK; the loop is only started
	// when SYNC_RECONCILE_ENABLED=true). The direct-API platforms are read via
	// their RemoteFetcher; Yandex is fetched over NATS by the reconciler itself.
	remoteFetchers := map[string]platform.RemoteFetcher{
		a2a.AgentTelegram: telegramSyncer,
		a2a.AgentVK:       vkSyncer,
	}
	s.Reconciler = service.NewReconciliationService(
		repos.SyncState,
		repos.Integration,
		repos.Business,
		h.NATS,
		remoteFetchers,
		s.PlanResolver,
	)

	s.ToolsCache = service.NewToolsRegistryCache(cfg.OrchestratorURL, nil, toolsCacheTTL)
	s.HITL = service.NewHITLService(
		h.PendingToolCallRepo,
		repos.Business,
		repos.Project,
		s.ToolsCache,
		orchClient,
	)

	if h.Redis != nil {
		s.Lockout = lockout.New(h.Redis, lockout.Config{
			FailThresholdCaptcha: cfg.LockoutFailThresholdCaptcha,
			FailThresholdLock:    cfg.LockoutFailThresholdLock,
			Duration:             cfg.LockoutDuration,
		})
		log.Info("lockout: enabled",
			"captcha_threshold", cfg.LockoutFailThresholdCaptcha,
			"lock_threshold", cfg.LockoutFailThresholdLock,
			"duration", cfg.LockoutDuration,
		)
	} else {
		log.Warn("lockout: disabled (no Redis client) — /auth/login will not enforce brute-force protection")
	}

	s.PasswordReset = service.NewPasswordResetService(
		h.PG,
		repos.PasswordResetToken,
		repos.UserResetExt,
		repos.EmailOutbox,
		s.AuditLogger,
		h.Redis,
		s.Lockout,
		cfg.PublicURL,
	)

	s.EmailVerification = service.NewEmailVerificationService(
		h.PG,
		repos.EmailVerificationToken,
		repos.UserResetExt,
		repos.EmailOutbox,
		h.Redis,
		cfg.PublicURL,
	)

	s.AccountDeletion = service.NewAccountDeletionService(
		h.PG,
		repos.UserResetExt,
		repos.Conversation,
		repos.EmailOutbox,
		s.AuditLogger,
	)

	s.BusinessDeletion = service.NewBusinessDeletionService(
		h.PG,
		repos.BusinessDeletionExt,
		repos.BusinessMembership,
		repos.User,
		repos.Conversation,
		h.PendingToolCallRepo,
		repos.EmailOutbox,
		s.AuditLogger,
	)
	s.BusinessDeletion.SetObjectStore(s.ObjectStorage)

	s.Consent = service.NewConsentService(
		h.PG,
		repos.UserConsents,
		s.AccountDeletion,
		s.AuditLogger,
		func(slug legalconfig.PolicySlug) (string, string) {
			return legalconfig.CurrentVersion(slug), ""
		},
	)

	s.Telemetry = service.NewTelemetryService(repos.TelemetryEvent)
	s.Feedback = service.NewFeedbackService(h.PG, repos.ProductFeedback, repos.EmailOutbox, repos.User, cfg.FeedbackNotifyEmail)

	if registerSetter, ok := s.User.(interface {
		SetRegisterCollaborators(
			pool service.RegisterTxPool,
			userRepo service.RegisterUserExt,
			consents service.ConsentInserter,
			verify service.RegisterVerifyIssuer,
			auditLogger audit.Logger,
		)
	}); ok {
		registerSetter.SetRegisterCollaborators(
			h.PG,
			repos.UserResetExt,
			repos.UserConsents,
			s.EmailVerification,
			s.AuditLogger,
		)
	}
	if consentSetter, ok := s.User.(interface {
		SetRegisterConsentService(consentSvc *service.ConsentService)
	}); ok {
		consentSetter.SetRegisterConsentService(s.Consent)
	}

	if err := middleware.InitTrustedProxies(cfg.TrustedProxyCIDRs); err != nil {
		return nil, fmt.Errorf("wire: init trusted proxies: %w", err)
	}
	if cfg.SmartCaptchaSecretKey != "" {
		s.SmartCaptcha = service.NewYandexSmartCaptcha(cfg.SmartCaptchaSecretKey, nil)
		log.Info("smartcaptcha: enabled (Yandex)")
	} else {
		s.SmartCaptcha = service.NewNoopSmartCaptcha()
		log.Warn("smartcaptcha: disabled (no SMARTCAPTCHA_SECRET_KEY) — captcha tier will not gate logins")
	}

	return s, nil
}

// buildTitlerRouter assembles the LLM Router that backs the auto-titler. It
// returns (nil, nil) when the titler is disabled (no TITLER_MODEL, or no
// provider API key) so the caller leaves Services.Titler nil and titling
// degrades gracefully.
//
// The Router is wired with three concerns, in order:
//   - provider options + pricing registry (LLMProviderOpts),
//   - the per-business daily-spend rate limiter (when Redis + billing present),
//   - the billing Writer (WithBilling) so every titler completion lands a
//     usage_logs row.
//
// WithBilling is load-bearing: pkg/llm/router.go gates logBilling on a non-nil
// billing sink, so omitting it (the pre-fix state) silently drops every titler
// usage_logs row. That under-counts the per-business daily-spend cap
// (billingRepository.GetDailySpend sums usage_logs) by the entire titler
// volume and under-reports real LLM spend in forensics. Mirrors the
// orchestrator's chat-path Router, which is likewise wired WithBilling.
// extraOpts is appended last so tests can inject a fake Selector (mirrors the
// orchestrator's wire.LLMRouter seam); production passes none.
func buildTitlerRouter(cfg *config.Config, log *slog.Logger, rdb *goredis.Client, billing llm.BillingRepository, extraOpts ...llm.RouterOption) (*llm.Router, error) {
	if cfg.TitlerModel == "" {
		log.Warn("auto-titler: disabled (TITLER_MODEL and LLM_MODEL both unset)")
		return nil, nil
	}

	registry := llm.NewRegistry()
	routerOpts := LLMProviderOpts(cfg, registry, log)
	if len(routerOpts) == 0 {
		log.Warn("auto-titler: disabled (no LLM provider API key set; set OPENROUTER_API_KEY, OPENAI_API_KEY, or ANTHROPIC_API_KEY to enable)")
		return nil, nil
	}

	if rdb != nil && billing != nil {
		rl, err := BuildAPIRateLimiter(cfg, log, rdb, billing)
		if err != nil {
			return nil, fmt.Errorf("api rate limiter: %w", err)
		}
		routerOpts = append(routerOpts, llm.WithRateLimiter(rl))
		log.Info("api rate limiter wired",
			"policy", cfg.RedisDownPolicy,
			"free_tier_daily_spend_usd", cfg.FreeTierDailySpendUSD,
		)
	} else {
		log.Warn("api rate limiter disabled — Redis or billing repo unavailable")
	}

	if billing != nil {
		routerOpts = append(routerOpts, llm.WithBilling(billing))
		log.Info("auto-titler: billing wired — titler completions write usage_logs")
	} else {
		log.Warn("auto-titler: billing disabled — titler LLM spend will not be recorded in usage_logs and the daily-spend cap will under-count")
	}

	routerOpts = append(routerOpts, extraOpts...)
	log.Info("auto-titler: llm router constructed", "model", cfg.TitlerModel, "providers", len(routerOpts))
	return llm.NewRouter(registry, routerOpts...), nil
}

// toolsCacheTTL caps how long the orchestrator tool registry is memoized
// for approval-validation lookups.
const toolsCacheTTL = 5 * time.Minute

// userResolverAdapter implements pkg/audit.UserResolver by delegating to
// domain.UserRepository.GetByID. Defined here (not pkg/audit) so pkg/audit
// stays free of the services/api/repository import.
type userResolverAdapter struct {
	repo domain.UserRepository
}

// EmailByID returns the user's current email; "" + nil on user-not-found so
// a deleted-mid-flight user doesn't surface as a resolver error.
func (a userResolverAdapter) EmailByID(ctx context.Context, userID uuid.UUID) (string, error) {
	u, err := a.repo.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return "", nil
		}
		return "", err
	}
	return u.Email, nil
}
