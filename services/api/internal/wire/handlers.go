package wire

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/ratelimit"
	"github.com/f1xgun/onevoice/pkg/ssecounter"
	"github.com/f1xgun/onevoice/services/api/internal/config"
	"github.com/f1xgun/onevoice/services/api/internal/handler"
	"github.com/f1xgun/onevoice/services/api/internal/handler/connect"
	"github.com/f1xgun/onevoice/services/api/internal/handler/oauth"
	"github.com/f1xgun/onevoice/services/api/internal/router"
	"github.com/f1xgun/onevoice/services/api/internal/service"
	"github.com/f1xgun/onevoice/services/api/internal/service/chatturn"
)

// sseKeyTTLSlack is added to the chat stream budget when sizing the SSE
// concurrency slot-key TTL so the key always outlives a single stream even
// under clock skew or a slow final flush.
const sseKeyTTLSlack = 5 * time.Minute

// init wires handler.SoleOwnerExtractor so UserDeletionHandler can return
// the 409 body with the businesses payload without importing service
// directly (which would force service to re-import a handler-side
// definition). See docs/api/wire-handlers.md.
func init() {
	handler.SoleOwnerExtractor = func(err error) ([]handler.SoleOwnerEntry, bool) {
		var soleErr *service.ErrSoleOwnerBusinesses
		if !errors.As(err, &soleErr) {
			return nil, false
		}
		out := make([]handler.SoleOwnerEntry, len(soleErr.Businesses))
		for i, b := range soleErr.Businesses {
			out[i] = handler.SoleOwnerEntry{ID: b.ID, Name: b.Name}
		}
		return out, true
	}
}

// Handlers constructs every HTTP handler for the API service and returns
// them aggregated in *router.Handlers ready for router.Setup. See
// docs/api/wire-handlers.md for the construction order + setter rationale.
func Handlers(cfg *config.Config, svcs *Services, repos *Repos, h *DBHandles) (*router.Handlers, error) {
	oauthHandler := oauth.NewOAuthHandler(svcs.OAuth, svcs.Integration, svcs.Business, oauth.OAuthConfig{
		VKClientID:         cfg.VKClientID,
		VKClientSecret:     cfg.VKClientSecret,
		VKRedirectURI:      cfg.VKRedirectURI,
		VKServiceKey:       cfg.VKServiceKey,
		YandexClientID:     cfg.YandexClientID,
		YandexClientSecret: cfg.YandexClientSecret,
		YandexRedirectURI:  cfg.YandexRedirectURI,
		GoogleClientID:     cfg.GoogleClientID,
		GoogleClientSecret: cfg.GoogleClientSecret,
		GoogleRedirectURI:  cfg.GoogleRedirectURI,
	}, nil, h.Redis)
	oauthHandler.WithSecureCookies(cfg.SecureCookies)
	if svcs.AgentTaskPublisher != nil {
		oauthHandler.WithAgentTaskPublisher(svcs.AgentTaskPublisher)
	}

	connectHandler := connect.NewConnectHandler(
		svcs.Integration,
		svcs.Business,
		connect.ConnectConfig{
			TelegramBotToken: cfg.TelegramBotToken,
			VKServiceKey:     cfg.VKServiceKey,
		},
		&http.Client{Timeout: 10 * time.Second},
	)

	internalTokenHandler := handler.NewInternalTokenHandler(svcs.Integration)

	internalBillingHandler := handler.NewInternalBillingHandler(repos.Billing, nil)

	authHandler, err := handler.NewAuthHandler(svcs.User, cfg.SecureCookies, svcs.AuditLogger, []byte(cfg.JWTSecret))
	if err != nil {
		return nil, fmt.Errorf("wire: create auth handler: %w", err)
	}
	if svcs.PasswordReset != nil {
		authHandler.SetPasswordResetService(svcs.PasswordReset)
	}
	if svcs.EmailVerification != nil {
		authHandler.SetEmailVerificationService(svcs.EmailVerification)
	}
	if repos.UserResetExt != nil {
		authHandler.SetMeUserExtraGetter(func(ctx context.Context, userID uuid.UUID) (*domain.User, error) {
			return repos.UserResetExt.GetByIDIncludingDeleted(ctx, userID)
		})
	}
	authHandler.WithLockout(svcs.Lockout, svcs.SmartCaptcha, cfg.SmartCaptchaFailOpen)
	businessHandler, err := handler.NewBusinessHandler(svcs.Business, svcs.PlatformSync, svcs.ObjectStorage)
	if err != nil {
		return nil, fmt.Errorf("wire: create business handler: %w", err)
	}
	integrationHandler, err := handler.NewIntegrationHandler(svcs.Integration, svcs.Business, svcs.AuditLogger)
	if err != nil {
		return nil, fmt.Errorf("wire: create integration handler: %w", err)
	}
	// Wire the proactive-sync drift/verify collaborators. Both are nil-safe in
	// the handler; the reconciler exists regardless of SYNC_RECONCILE_ENABLED so
	// the drift endpoint reads the table and verify re-pushes on demand.
	integrationHandler.SetReconciler(svcs.Reconciler, svcs.PlatformSync)
	reviewHandler, err := handler.NewReviewHandler(svcs.Review)
	if err != nil {
		return nil, fmt.Errorf("wire: create review handler: %w", err)
	}
	postHandler, err := handler.NewPostHandler(svcs.Post)
	if err != nil {
		return nil, fmt.Errorf("wire: create post handler: %w", err)
	}
	agentTaskHandler, err := handler.NewAgentTaskHandler(svcs.AgentTask, svcs.TaskHub)
	if err != nil {
		return nil, fmt.Errorf("wire: create agent task handler: %w", err)
	}

	projectHandler, err := handler.NewProjectHandler(svcs.Project, svcs.Business)
	if err != nil {
		return nil, fmt.Errorf("wire: create project handler: %w", err)
	}

	conversationHandler, err := handler.NewConversationHandler(repos.Conversation, repos.Message, svcs.Business, svcs.Project, svcs.Conversation)
	if err != nil {
		return nil, fmt.Errorf("wire: create conversation handler: %w", err)
	}

	chatProxyHandler := handler.NewChatProxyHandler(
		svcs.Business,
		svcs.Integration,
		svcs.Project,
		repos.Conversation,
		repos.Message,
		h.PendingToolCallRepo,
		repos.Post,
		repos.Review,
		repos.AgentTask,
		svcs.TaskHub,
		svcs.OrchClient,
		svcs.Titler,
		svcs.AuditLogger,
		cfg.MessageHistoryLimit,
	)
	// Forward the per-business rate-limit tier to the orchestrator (replaces the
	// legacy hardcoded empty tier). Set on the shared Turn so HITLHandler.Resume,
	// which reuses chatProxyHandler.Turn(), sees it too.
	chatProxyHandler.Turn().SetPlanResolver(svcs.PlanResolver)

	var sseCounter *ssecounter.Counter
	if h.Redis != nil && cfg.SSEMaxPerUser > 0 {
		policy, perr := ratelimit.PolicyFromEnv(cfg.RedisDownPolicy, cfg.LocalFallbackRequestsPerHour)
		if perr != nil {
			return nil, fmt.Errorf("wire: build ratelimit policy: %w", perr)
		}
		sseCounter = ssecounter.NewWithKeyTTL(h.Redis, cfg.SSEMaxPerUser, policy, chatturn.StreamBudget+sseKeyTTLSlack)
		chatProxyHandler.SetSSECounter(sseCounter, cfg.LLMTier)
	}

	hitlHandler, err := handler.NewHITLHandler(svcs.HITL, svcs.Business, repos.Conversation, repos.Integration, chatProxyHandler.Turn())
	if err != nil {
		return nil, fmt.Errorf("wire: create hitl handler: %w", err)
	}
	if sseCounter != nil {
		hitlHandler.SetSSECounter(sseCounter, cfg.LLMTier)
		reviewHandler.SetSSECounter(sseCounter, cfg.LLMTier)
	}

	businessHandler.SetToolsCache(svcs.ToolsCache)
	projectHandler.SetToolsCache(svcs.ToolsCache)

	titlerHandler := handler.NewTitlerHandler(svcs.Titler, repos.Conversation, repos.Message, cfg.MessageHistoryLimit)

	searchHandler, err := handler.NewSearchHandler(svcs.Searcher)
	if err != nil {
		return nil, fmt.Errorf("wire: create search handler: %w", err)
	}

	platformsHandler := handler.NewPlatformsHandler(handler.PlatformAvailability{
		Telegram:       cfg.TelegramBotToken != "",
		VK:             cfg.VKClientID != "" && cfg.VKClientSecret != "",
		YandexBusiness: cfg.YandexClientID != "" && cfg.YandexClientSecret != "",
		GoogleBusiness: cfg.GoogleClientID != "" && cfg.GoogleClientSecret != "",
	})

	membersHandler, err := handler.NewMembersHandler(repos.BusinessMembership, repos.Role, repos.User, svcs.Business, h.PG, svcs.AuthzCache, svcs.AuditLogger)
	if err != nil {
		return nil, fmt.Errorf("wire: create members handler: %w", err)
	}
	rolesHandler, err := handler.NewRolesHandler(
		repos.Role,
		repos.BusinessMembership,
		svcs.Business,
		h.PG,
		svcs.AuthzCache,
		svcs.AuditLogger,
	)
	if err != nil {
		return nil, fmt.Errorf("wire: create roles handler: %w", err)
	}

	invitationsHandler, err := handler.NewInvitationsHandler(
		repos.Invitation,
		repos.BusinessMembership,
		repos.Role,
		repos.User,
		repos.Business,
		svcs.Business,
		h.PG,
		svcs.AuthzCache,
		svcs.AuditLogger,
	)
	if err != nil {
		return nil, fmt.Errorf("wire: create invitations handler: %w", err)
	}

	auditLister, ok := repos.AuditLog.(handler.AuditLogLister)
	if !ok {
		return nil, fmt.Errorf("wire: AuditLog repo does not satisfy handler.AuditLogLister (impl drift)")
	}
	auditLogHandler := handler.NewAuditLogHandler(auditLister)

	billingHandler := handler.NewBillingHandler(
		service.NewBillingSummaryService(svcs.PlanResolver, repos.Billing),
	)

	var userDeletionHandler *handler.UserDeletionHandler
	if svcs.AccountDeletion != nil {
		userDeletionHandler = handler.NewUserDeletionHandler(svcs.AccountDeletion, cfg.CORSAllowedOrigins)
	}

	var businessDeletionHandler *handler.BusinessDeletionHandler
	if svcs.BusinessDeletion != nil {
		businessDeletionHandler = handler.NewBusinessDeletionHandler(svcs.BusinessDeletion, cfg.CORSAllowedOrigins)
	}

	var consentsHandler *handler.ConsentsHandler
	if svcs.Consent != nil {
		var deletionSvc handler.AccountDeletionServiceAPI
		if svcs.AccountDeletion != nil {
			deletionSvc = svcs.AccountDeletion
		}
		consentsHandler = handler.NewConsentsHandler(svcs.Consent, deletionSvc, repos.UserConsents, cfg.CORSAllowedOrigins)
	}
	if svcs.Consent != nil {
		authHandler.SetConsentDiffer(svcs.Consent)
	}

	return &router.Handlers{
		Auth:             authHandler,
		Business:         businessHandler,
		Integration:      integrationHandler,
		Conversation:     conversationHandler,
		OAuth:            oauthHandler,
		Connect:          connectHandler,
		InternalToken:    internalTokenHandler,
		InternalBilling:  internalBillingHandler,
		ChatProxy:        chatProxyHandler,
		Review:           reviewHandler,
		Post:             postHandler,
		AgentTask:        agentTaskHandler,
		Project:          projectHandler,
		HITL:             hitlHandler,
		Titler:           titlerHandler,
		Search:           searchHandler,
		Platforms:        platformsHandler,
		Permissions:      handler.NewPermissionsHandler(),
		Members:          membersHandler,
		Roles:            rolesHandler,
		Invitations:      invitationsHandler,
		AuditLog:         auditLogHandler,
		Billing:          billingHandler,
		UserDeletion:     userDeletionHandler,
		BusinessDeletion: businessDeletionHandler,
		Consents:         consentsHandler,
		Telemetry:        handler.NewTelemetryHandler(svcs.Telemetry),
		Feedback:         handler.NewFeedbackHandler(svcs.Feedback),
	}, nil
}
