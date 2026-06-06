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
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/legalconfig"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/lockout"
	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
	"github.com/f1xgun/onevoice/services/api/internal/config"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/platform"
	"github.com/f1xgun/onevoice/services/api/internal/service"
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

	// Consent is wired into ConsentsHandler + into UserService.Register via
	// SetRegisterConsentService so the 3-row UPSERT runs in the same tx as
	// the user row.
	Consent *service.ConsentService

	// Lockout is non-nil whenever h.Redis is non-nil (Redis is the storage
	// layer). SmartCaptcha is always non-nil — Noop impl when
	// SMARTCAPTCHA_SECRET_KEY is empty so the handler has a stable
	// dependency to inject.
	Lockout      *lockout.Lockout
	SmartCaptcha service.SmartCaptchaVerifier

	// reviewSyncerCancel stops the background ticker. nil when ReviewSyncer
	// is nil.
	reviewSyncerCancel context.CancelFunc
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
}

// StartReviewSyncer starts the background review-syncer ticker. Idempotent
// no-op when the syncer is nil. The ticker stops when either the parent ctx
// cancels or Services.Close is called.
func (s *Services) StartReviewSyncer(ctx context.Context, log *slog.Logger, intervalMinutes int) {
	if s == nil || s.ReviewSyncer == nil {
		return
	}
	syncCtx, syncCancel := context.WithCancel(ctx)
	s.reviewSyncerCancel = syncCancel
	go s.ReviewSyncer.Start(syncCtx)
	log.Info("review syncer started", "interval_minutes", intervalMinutes)
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

	var llmRouter *llm.Router
	titlerModel := cfg.TitlerModel
	if titlerModel != "" {
		registry := llm.NewRegistry()
		routerOpts := LLMProviderOpts(cfg, registry, log)
		if len(routerOpts) > 0 {
			if h.Redis != nil && repos.Billing != nil {
				rl, rlErr := BuildAPIRateLimiter(cfg, log, h.Redis, repos.Billing)
				if rlErr != nil {
					return nil, fmt.Errorf("api rate limiter: %w", rlErr)
				}
				routerOpts = append(routerOpts, llm.WithRateLimiter(rl))
				log.Info("api rate limiter wired",
					"policy", cfg.RedisDownPolicy,
					"free_tier_daily_spend_usd", cfg.FreeTierDailySpendUSD,
				)
			} else {
				log.Warn("api rate limiter disabled — Redis or billing repo unavailable")
			}
			llmRouter = llm.NewRouter(registry, routerOpts...)
			log.Info("auto-titler: llm router constructed", "model", titlerModel, "providers", len(routerOpts))
		} else {
			log.Warn("auto-titler: disabled (no LLM provider API key set; set OPENROUTER_API_KEY, OPENAI_API_KEY, or ANTHROPIC_API_KEY to enable)")
		}
	} else {
		log.Warn("auto-titler: disabled (TITLER_MODEL and LLM_MODEL both unset)")
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
	s.Integration = service.NewIntegrationService(repos.Integration, h.Enc, refresher, s.AuditLogger)
	if h.NATS != nil {
		s.Integration = service.WithNATSPublisher(s.Integration, h.NATS)
	}
	s.OAuth = service.NewOAuthService(h.Redis)
	s.Post = service.NewPostService(repos.Post, s.Business)
	s.AgentTask = service.NewAgentTaskService(repos.AgentTask, s.Business)
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
	perPlatform := map[string]any{
		a2a.AgentTelegram:       platform.NewTelegramSyncer(adapter, platformHTTPClient, "", cfg.PublicURL),
		a2a.AgentVK:             platform.NewVKSyncer(adapter, platformHTTPClient, ""),
		a2a.AgentYandexBusiness: platform.NewYandexSyncer(yandexPublisher),
	}
	s.PlatformSync = platform.NewSyncer(adapter, repos.AgentTask, s.TaskHub, perPlatform)

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

	s.Consent = service.NewConsentService(
		h.PG,
		repos.UserConsents,
		s.AccountDeletion,
		s.AuditLogger,
		func(slug legalconfig.PolicySlug) (string, string) {
			return legalconfig.CurrentVersion(slug), ""
		},
	)

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
