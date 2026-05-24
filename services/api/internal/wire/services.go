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
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
	"github.com/f1xgun/onevoice/services/api/internal/config"
	"github.com/f1xgun/onevoice/services/api/internal/platform"
	"github.com/f1xgun/onevoice/services/api/internal/service"
	"github.com/f1xgun/onevoice/services/api/internal/storage"
	"github.com/f1xgun/onevoice/services/api/internal/taskhub"
)

// Services aggregates every business-logic service the API consumes.
//
// Optional services (Titler, Searcher, ReviewSyncer, PlatformSync) are
// nil-safe — downstream consumers already nil-guard. NATS-dependent
// services are only constructed when h.NATS is non-nil.
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

	// OrchClient is the shared HTTP client for orchestrator endpoints
	// (chat / resume / internal tool registry). Extracted from the inline
	// plumbing in service/hitl.go so chatproxy collaborators and
	// handler/hitl.go consume the same client.
	OrchClient *orchestratorclient.Client

	// AgentTaskPublisher is exposed so handlers (specifically OAuthHandler)
	// can call WithAgentTaskPublisher when NATS is available. nil when NATS
	// is unreachable.
	AgentTaskPublisher *platform.NATSTaskPublisher

	// AuthzCache backs the RequireBusinessAccess middleware
	// (Phase 2 v2.0 RBAC). Non-nil — the middleware panics on a nil cache
	// at request time.
	AuthzCache *authz.Cache

	// AuditLogger is the fire-and-forget async writer for audit_logs
	// (Phase 19 Wave 2). Constructed once here and injected into every
	// service / handler that records security-sensitive mutations
	// (wiring done in Plan 19-04). Safe to call from any goroutine; never
	// blocks the request path.
	AuditLogger audit.Logger

	// PasswordReset is the Phase 21b (ACCT-01) password-reset service.
	// Wired into AuthHandler via SetPasswordResetService in
	// wire/handlers.go.
	PasswordReset *service.PasswordResetService

	// reviewSyncerCancel is captured so Close() can stop the background
	// ticker goroutine. nil when ReviewSyncer is nil.
	reviewSyncerCancel context.CancelFunc
}

// Close stops background goroutines (review syncer ticker) and is safe to
// call multiple times. NATS connection lifecycle stays with DBHandles.
func (s *Services) Close() {
	if s == nil {
		return
	}
	if s.reviewSyncerCancel != nil {
		s.reviewSyncerCancel()
	}
}

// StartReviewSyncer starts the background review-syncer ticker. Idempotent:
// returns nil and does nothing if the syncer is nil. The ticker stops when
// either the parent ctx cancels or Services.Close() is called.
//
// Kept separate from Services() so cmd/main.go can defer Close() before the
// ticker starts (current behavior preserved verbatim from the legacy
// inline `if reviewSyncer != nil { go reviewSyncer.Start(syncCtx) }` block).
func (s *Services) StartReviewSyncer(ctx context.Context, log *slog.Logger, intervalMinutes int) {
	if s == nil || s.ReviewSyncer == nil {
		return
	}
	syncCtx, syncCancel := context.WithCancel(ctx)
	s.reviewSyncerCancel = syncCancel
	go s.ReviewSyncer.Start(syncCtx)
	log.Info("review syncer started", "interval_minutes", intervalMinutes)
}

// BuildServices constructs the business-logic layer.
//
// Construction order matters: Titler depends on the LLM router; Searcher's
// readiness flag must flip AFTER BootstrapDatabases ensured the search
// indexes exist (the atomic.Bool.Store provides a happens-before edge for
// every subsequent Load by handler goroutines).
//
// Named BuildServices (not Services) because Go forbids a type and a
// function with identical identifiers in one package — Services is the
// returned aggregate type. Callers spell the call site as
// `wire.BuildServices(...)`.
func BuildServices(ctx context.Context, log *slog.Logger, cfg *config.Config, repos *Repos, h *DBHandles) (*Services, error) {
	// Build the shared orchestrator client BEFORE any service that talks to
	// the orchestrator. SSE consumers (HITLService.OrchClient,
	// chatproxy.OrchestrationProxy) require Timeout=0 so streaming requests
	// are not killed mid-flight; the per-call ctx still bounds the budget.
	orchClient := orchestratorclient.New(cfg.OrchestratorURL, &http.Client{Timeout: 0})

	s := &Services{
		TaskHub:    taskhub.New(),
		OrchClient: orchClient,
		// Phase 19 Wave 2: audit logger constructed once here and shared
		// across every service / handler that records security-sensitive
		// mutations. Async + bounded retry + metric-on-failure live inside
		// pkg/audit; consumers just call AuditLogger.Log(ctx, entry).
		//
		// Phase 21-03 / ACCT-06: NewLoggerWithResolver injects a tiny
		// adapter wrapping UserRepository.GetByID so loggerImpl.write can
		// snapshot user_email_at_event BEFORE the INSERT. After Phase 21-04
		// hard-deletes a user, the audit row's FK becomes NULL but the
		// email survives for 152-ФЗ forensic queries.
		AuditLogger: audit.NewLoggerWithResolver(repos.AuditLog, userResolverAdapter{repo: repos.User}),
	}

	// Auto-titler LLM Router wiring.
	//
	// Graceful disable: when no LLM provider key is set OR no model is
	// configured, the titler is left nil and downstream
	// fireAutoTitleIfPending becomes a no-op. The API service must boot in
	// dev environments without any LLM env at all.
	var llmRouter *llm.Router
	titlerModel := cfg.TitlerModel
	if titlerModel != "" {
		registry := llm.NewRegistry()
		routerOpts := LLMProviderOpts(cfg, registry, log)
		if len(routerOpts) > 0 {
			llmRouter = llm.NewRouter(registry, routerOpts...)
			log.Info("auto-titler: llm router constructed", "model", titlerModel, "providers", len(routerOpts))
		} else {
			log.Warn("auto-titler: disabled (no LLM provider API key set; set OPENROUTER_API_KEY, OPENAI_API_KEY, or ANTHROPIC_API_KEY to enable)")
		}
	} else {
		log.Warn("auto-titler: disabled (TITLER_MODEL and LLM_MODEL both unset)")
	}

	// Construct the Titler service when the Router is available. Stays nil
	// on the graceful-disable branch so fireAutoTitleIfPending becomes a
	// no-op.
	if llmRouter != nil {
		s.Titler = service.NewTitler(llmRouter, repos.Conversation, titlerModel)
		log.Info("auto-titler: service constructed", "model", titlerModel)
	}

	// Search service.
	//
	// CRITICAL ORDERING: MarkIndexesReady is called HERE, AFTER
	// BootstrapDatabases.EnsureSearchIndexes has already returned nil. The
	// atomic.Bool's Store ensures a happens-before edge against every
	// subsequent Load by handler goroutines. Reordering this would cause the
	// readiness flag to flip before indexes exist — Searcher.Search would
	// no longer return ErrSearchIndexNotReady on a cold boot, and queries
	// would hit a missing $text index.
	s.Searcher = service.NewSearcher(repos.Conversation, repos.Message)
	s.Searcher.MarkIndexesReady()

	// Initialize core services
	userService, err := service.NewUserService(repos.User, h.Redis, cfg.JWTSecret)
	if err != nil {
		return nil, fmt.Errorf("wire: create user service: %w", err)
	}
	s.User = userService
	// Phase 1 v2.0 RBAC (DATA-06): BusinessService dual-writes
	// businesses + business_members(role_id=owner) inside a single tx.
	// Phase 19 Wave 4 (19-04): + AuditLogger emits business.created/updated
	// AFTER tx.Commit succeeds (D-29/D-30).
	s.Business = service.NewBusinessService(repos.Business, repos.BusinessMembership, repos.Role, h.PG, s.AuditLogger)

	// Phase 2 v2.0 RBAC: authz cache backs the RequireBusinessAccess
	// middleware. The MembershipLoader queries (user_id, business_id) →
	// (role_id, permissions) in one round-trip; the cache memoizes results
	// keyed by (user_id, business_id) with explicit invalidation on member
	// add/remove/role-change.
	s.AuthzCache = authz.NewCache(repos.MembershipLoader)

	// Build Google token refresher if credentials are configured. The
	// refresher is wired into IntegrationService below so token refresh
	// happens transparently inside GetDecryptedToken.
	var refresher service.TokenRefresher
	if cfg.GoogleClientID != "" && cfg.GoogleClientSecret != "" {
		refresher = NewGoogleTokenRefresher(
			cfg.GoogleClientID,
			cfg.GoogleClientSecret,
			&http.Client{Timeout: cfg.OrchestratorFetchTimeout},
		)
	}
	// Phase 19 Wave 4 (19-04): IntegrationService emits integration.connected
	// and integration.token_rotated. ProjectService emits project.* events.
	s.Integration = service.NewIntegrationService(repos.Integration, h.Enc, refresher, s.AuditLogger)
	s.OAuth = service.NewOAuthService(h.Redis)
	s.Post = service.NewPostService(repos.Post, s.Business)
	s.AgentTask = service.NewAgentTaskService(repos.AgentTask, s.Business)
	s.Project = service.NewProjectService(repos.Project, s.AuditLogger)

	// ConversationService owns multi-repo conversation transitions
	// (currently MoveToProject — see services/api/internal/service/conversation.go).
	// Reads from three repos so MoveConversation handler can shrink to a
	// thin HTTP-to-domain-call adapter.
	conversationService, err := service.NewConversationService(repos.Conversation, repos.Message, repos.Project, h.PendingToolCallRepo)
	if err != nil {
		return nil, fmt.Errorf("wire: create conversation service: %w", err)
	}
	s.Conversation = conversationService

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
		return nil, fmt.Errorf("wire: init object storage: %w", err)
	}
	s.ObjectStorage = objectStorage
	log.Info("connected to object storage", "endpoint", cfg.S3Endpoint, "bucket", cfg.S3Bucket)

	// Review syncer: built early so it can be injected into reviewService
	// for the manual-refresh endpoint. The background ticker is started by
	// Services.StartReviewSyncer (called from cmd/main.go after handlers
	// are wired). With h.NATS=nil the syncer is nil and Refresh returns an
	// error on the handler path, which is acceptable for legacy/test envs.
	if h.NATS != nil {
		var drafter service.DraftPassRunner
		if cfg.ReviewDraftEnabled {
			// Drafter consumes the shared orchestrator client so its calls
			// reuse the same Transport pool as everything else (HITL,
			// chatproxy, tools cache).
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

	// Review service: needs h.NATS to dispatch manual replies (PUT
	// /reviews/{id}/reply) to platform agents — vk__reply_comment etc. With
	// h.NATS=nil the service still works in Mongo-only mode (legacy).
	// reviewSyncer powers POST /reviews/refresh; nil disables it.
	var reviewRefresher service.ReviewRefresher
	if s.ReviewSyncer != nil {
		reviewRefresher = s.ReviewSyncer
	}
	s.Review = service.NewReviewService(repos.Review, s.Business, h.NATS, reviewRefresher)

	// Platform syncer: pushes business info updates to connected platforms.
	// Each capability-implementing platform impl is wired into perPlatform
	// keyed by integ.Platform; Syncer dispatches to whichever interfaces
	// the impl satisfies (TitleSyncer, DescriptionSyncer, PhotoSyncer,
	// InfoSyncer, ScheduleSyncer) — no no-op methods required.
	adapter := IntegrationSyncAdapter(s.Integration)
	platformHTTPClient := &http.Client{Timeout: 10 * time.Second}
	if h.NATS != nil {
		s.AgentTaskPublisher = platform.NewNATSTaskPublisher(h.NATS)
	}
	// AgentTaskPublisher is *platform.NATSTaskPublisher (nil-typed when
	// h.NATS is nil); cast through the platform.TaskPublisher interface
	// so YandexSyncer's nil-check sees an honestly-nil value.
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

	// HITL services. ToolsRegistryCache talks to the orchestrator's
	// /internal/tools endpoint with a 5-min TTL so settings/project pages
	// + edit-validation share one source of truth.
	s.ToolsCache = service.NewToolsRegistryCache(cfg.OrchestratorURL, nil, toolsCacheTTL)
	s.HITL = service.NewHITLService(
		h.PendingToolCallRepo,
		repos.Business,
		repos.Project,
		s.ToolsCache,
		orchClient,
	)

	// Phase 21b (ACCT-01) password reset service. Composes the
	// PasswordResetTokenRepository + the tx-aware user repo adapter +
	// the email outbox (Phase 21a) + the shared audit logger + Redis
	// (rate-limit + post-commit refresh-token wipe).
	s.PasswordReset = service.NewPasswordResetService(
		h.PG,
		repos.PasswordResetToken,
		repos.UserResetExt,
		repos.EmailOutbox,
		s.AuditLogger,
		h.Redis,
	)

	return s, nil
}

// toolsCacheTTL — how long the orchestrator tool registry is cached for
// approval-validation lookups. 5 minutes balances responsiveness to
// orchestrator restarts against load on /internal/tools.
const toolsCacheTTL = 5 * time.Minute

// userResolverAdapter implements pkg/audit.UserResolver by delegating to
// domain.UserRepository.GetByID. Defined locally in wire/ (not pkg/audit)
// to keep pkg/audit free of the services/api/repository import.
//
// On lookup failure the adapter returns ("", err) — pkg/audit.loggerImpl
// catches the error, slog.Warns, and leaves UserEmailAtEvent empty so the
// audit row still writes (Phase 21-03 / ACCT-06 D-disposition).
type userResolverAdapter struct {
	repo domain.UserRepository
}

// EmailByID returns the user's current email; "" + nil on user-not-found
// so a deleted-mid-flight user doesn't surface as a resolver error.
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
