package wire

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
	"github.com/f1xgun/onevoice/services/api/internal/config"
	"github.com/f1xgun/onevoice/services/api/internal/platform"
	"github.com/f1xgun/onevoice/services/api/internal/service"
	"github.com/f1xgun/onevoice/services/api/internal/storage"
	"github.com/f1xgun/onevoice/services/api/internal/taskhub"
)

// Per-call HTTP timeout for service-to-API integration calls. The
// orchestrator-fetch budget is env-tunable per cfg.OrchestratorFetchTimeout
// (ORCHESTRATOR_FETCH_TIMEOUT). integrationFetchTimeout stays a const because
// it has not (yet) been promoted to env-driven configuration.
const integrationFetchTimeout = 60 * time.Second

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
	HITL          *service.HITLService
	Titler        *service.Titler // may be nil — graceful disable
	Searcher      *service.Searcher
	ToolsCache    *service.ToolsRegistryCache
	PlatformSync  *platform.Syncer
	ReviewSyncer  *service.ReviewSyncer
	TaskHub       *taskhub.Hub
	ObjectStorage *storage.MinioClient

	// OrchClient is the shared HTTP client for orchestrator endpoints
	// (chat / resume / internal tool registry). Phase 19 / D-11 — extracted
	// from the inline plumbing in service/hitl.go so chatproxy collaborators
	// (Plan 19-03) and handler/hitl.go consume the same client.
	OrchClient *orchestratorclient.Client

	// AgentTaskPublisher is exposed so handlers (specifically OAuthHandler)
	// can call WithAgentTaskPublisher when NATS is available. nil when NATS
	// is unreachable.
	AgentTaskPublisher *platform.NATSTaskPublisher

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
// every subsequent Load by handler goroutines — see RESEARCH §7).
//
// Named BuildServices (not Services) because Go forbids a type and a
// function with identical identifiers in one package — Services is the
// returned aggregate type. Callers spell the call site as
// `wire.BuildServices(...)`.
func BuildServices(ctx context.Context, log *slog.Logger, cfg *config.Config, repos *Repos, h *DBHandles) (*Services, error) {
	// Phase 19 / D-11: build the shared orchestrator client BEFORE any service
	// that talks to the orchestrator. SSE consumers (HITLService.OrchClient,
	// chatproxy.OrchestrationProxy) require Timeout=0 so streaming requests
	// are not killed mid-flight; the per-call ctx still bounds the budget.
	orchClient := orchestratorclient.New(cfg.OrchestratorURL, &http.Client{Timeout: 0})

	s := &Services{
		TaskHub:    taskhub.New(),
		OrchClient: orchClient,
	}

	// Phase 18 — Auto-titler LLM Router wiring (Plan 18-02).
	//
	// Graceful disable per Pitfall 1 / Assumption A6: when no LLM provider
	// key is set OR no model is configured, the titler is left nil and
	// downstream Plan 18-05's fireAutoTitleIfPending becomes a no-op. The
	// API service must boot in dev environments without any LLM env at all.
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

	// Phase 18 Plan 04 — construct the Titler service when the Router is
	// available. Stays nil on the graceful-disable branch so Plan 05's
	// fireAutoTitleIfPending becomes a no-op (Pitfall 1 / Assumption A6).
	if llmRouter != nil {
		s.Titler = service.NewTitler(llmRouter, repos.Conversation, titlerModel)
		log.Info("auto-titler: service constructed", "model", titlerModel)
	}

	// Phase 19 Plan 19-03 — Search service.
	//
	// CRITICAL ORDERING (T-19-INDEX-503 mitigation): MarkIndexesReady is
	// called HERE, AFTER BootstrapDatabases.EnsureSearchIndexes has already
	// returned nil. The atomic.Bool's Store ensures a happens-before edge
	// against every subsequent Load by handler goroutines. Reordering this
	// would cause the readiness flag to flip before indexes exist —
	// Searcher.Search would no longer return ErrSearchIndexNotReady on a
	// cold boot, and queries would hit a missing $text index.
	s.Searcher = service.NewSearcher(repos.Conversation, repos.Message)
	s.Searcher.MarkIndexesReady()

	// Initialize core services
	userService, err := service.NewUserService(repos.User, h.Redis, cfg.JWTSecret)
	if err != nil {
		return nil, fmt.Errorf("wire: create user service: %w", err)
	}
	s.User = userService
	s.Business = service.NewBusinessService(repos.Business)

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
	s.Integration = service.NewIntegrationService(repos.Integration, h.Enc, refresher)
	s.OAuth = service.NewOAuthService(h.Redis)
	s.Post = service.NewPostService(repos.Post, s.Business)
	s.AgentTask = service.NewAgentTaskService(repos.AgentTask, s.Business)
	s.Project = service.NewProjectService(repos.Project)

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
			drafter = service.NewReviewDrafter(
				repos.Review,
				repos.Business,
				&http.Client{Timeout: integrationFetchTimeout},
				cfg.OrchestratorURL,
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

	// Plan 16-07 HITL services. ToolsRegistryCache talks to the
	// orchestrator's /internal/tools endpoint with a 5-min TTL so
	// settings/project pages + edit-validation share one source of truth.
	s.ToolsCache = service.NewToolsRegistryCache(cfg.OrchestratorURL, nil, toolsCacheTTL)
	s.HITL = service.NewHITLService(
		h.PendingToolCallRepo,
		repos.Business,
		repos.Project,
		s.ToolsCache,
		orchClient,
	)

	return s, nil
}

// toolsCacheTTL — how long the orchestrator tool registry is cached for
// approval-validation lookups. 5 minutes balances responsiveness to
// orchestrator restarts against load on /internal/tools.
const toolsCacheTTL = 5 * time.Minute
