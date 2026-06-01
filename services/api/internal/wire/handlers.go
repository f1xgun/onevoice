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
)

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
	if svcs.AgentTaskPublisher != nil {
		oauthHandler.WithAgentTaskPublisher(svcs.AgentTaskPublisher)
	}

	// paste-flow only — narrow ConnectConfig (no OAuth client credentials).
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

	// repos.Billing satisfies handler.BillingService via LogUsage.
	internalBillingHandler := handler.NewInternalBillingHandler(repos.Billing, nil)

	// AuthHandler takes audit + jwtSecret so it can emit auth.* audit events
	// and extract userID from refresh-token claims before Redis invalidation
	// during Logout.
	authHandler, err := handler.NewAuthHandler(svcs.User, cfg.SecureCookies, svcs.AuditLogger, []byte(cfg.JWTSecret))
	if err != nil {
		return nil, fmt.Errorf("wire: create auth handler: %w", err)
	}
	// setter pattern keeps NewAuthHandler's signature stable across phases
	if svcs.PasswordReset != nil {
		authHandler.SetPasswordResetService(svcs.PasswordReset)
	}
	if svcs.EmailVerification != nil {
		authHandler.SetEmailVerificationService(svcs.EmailVerification)
	}
	// /auth/me must surface deletion state via GetByIDIncludingDeleted so
	// soft-deleted users can render the grace banner + click restore.
	if repos.UserResetExt != nil {
		authHandler.SetMeUserExtraGetter(func(ctx context.Context, userID uuid.UUID) (*domain.User, error) {
			return repos.UserResetExt.GetByIDIncludingDeleted(ctx, userID)
		})
	}
	// lockout + SmartCaptcha verifier — both nil-safe inside AuthHandler.Login
	authHandler.WithLockout(svcs.Lockout, svcs.SmartCaptcha, cfg.SmartCaptchaFailOpen)
	businessHandler, err := handler.NewBusinessHandler(svcs.Business, svcs.PlatformSync, svcs.ObjectStorage)
	if err != nil {
		return nil, fmt.Errorf("wire: create business handler: %w", err)
	}
	// + svcs.AuditLogger for integration.disconnected audit events
	integrationHandler, err := handler.NewIntegrationHandler(svcs.Integration, svcs.Business, svcs.AuditLogger)
	if err != nil {
		return nil, fmt.Errorf("wire: create integration handler: %w", err)
	}
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

	projectHandler, err := handler.NewProjectHandler(svcs.Project)
	if err != nil {
		return nil, fmt.Errorf("wire: create project handler: %w", err)
	}

	// depends on business + project services for create-conversation
	// scoping; svcs.Conversation owns the /move endpoint + GET /messages view.
	conversationHandler, err := handler.NewConversationHandler(repos.Conversation, repos.Message, svcs.Business, svcs.Project, svcs.Conversation)
	if err != nil {
		return nil, fmt.Errorf("wire: create conversation handler: %w", err)
	}

	// chat proxy needs projectService + conversationRepo for the project_*
	// enrichment on every /chat/{id} request.
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
		svcs.OrchClient, // single shared orchestrator client
		svcs.Titler,     // nil when titling disabled
	)

	// Per-user SSE concurrency cap. SSE_MAX_PER_USER=0 disables. Tests
	// construct without Redis (h.Redis == nil) — skip rather than fail closed.
	if h.Redis != nil && cfg.SSEMaxPerUser > 0 {
		policy, perr := ratelimit.PolicyFromEnv(cfg.RedisDownPolicy, cfg.LocalFallbackRequestsPerHour)
		if perr != nil {
			return nil, fmt.Errorf("wire: build ratelimit policy: %w", perr)
		}
		sseCounter := ssecounter.New(h.Redis, cfg.SSEMaxPerUser, policy)
		chatProxyHandler.SetSSECounter(sseCounter, cfg.LLMTier)
	}

	hitlHandler, err := handler.NewHITLHandler(svcs.HITL, svcs.Business, repos.Conversation)
	if err != nil {
		return nil, fmt.Errorf("wire: create hitl handler: %w", err)
	}

	// shared ToolsRegistryCache so PUT /tool-approvals + PUT /projects/{id}
	// validate approval-overrides against the live orchestrator registry.
	businessHandler.SetToolsCache(svcs.ToolsCache)
	projectHandler.SetToolsCache(svcs.ToolsCache)

	// svcs.Titler may be nil (graceful disable) — handler returns 503.
	// conversationRepo + messageRepo are required (panic-on-nil).
	titlerHandler := handler.NewTitlerHandler(svcs.Titler, repos.Conversation, repos.Message)

	// readiness flag already flipped by wire.BuildServices
	searchHandler, err := handler.NewSearchHandler(svcs.Searcher)
	if err != nil {
		return nil, fmt.Errorf("wire: create search handler: %w", err)
	}

	// availability derived from cfg so platforms missing credentials surface
	// as oauth_not_configured to the frontend.
	platformsHandler := handler.NewPlatformsHandler(handler.PlatformAvailability{
		Telegram:       cfg.TelegramBotToken != "",
		VK:             cfg.VKClientID != "" && cfg.VKClientSecret != "",
		YandexBusiness: cfg.YandexClientID != "" && cfg.YandexClientSecret != "",
		GoogleBusiness: cfg.GoogleClientID != "" && cfg.GoogleClientSecret != "",
	})

	// + svcs.AuditLogger for rbac.role_granted + rbac.member_removed events
	membersHandler, err := handler.NewMembersHandler(repos.BusinessMembership, repos.Role, repos.User, h.PG, svcs.AuthzCache, svcs.AuditLogger)
	if err != nil {
		return nil, fmt.Errorf("wire: create members handler: %w", err)
	}
	// + membership repo (Delete fanout target lookup) + pool (RepeatableRead
	// tx) + invalidator (InvalidateRole after commit + InvalidateMember
	// fanout per reassigned user) + AuditLogger (rbac.role_* events).
	rolesHandler, err := handler.NewRolesHandler(
		repos.Role,
		repos.BusinessMembership,
		h.PG,
		svcs.AuthzCache,
		svcs.AuditLogger,
	)
	if err != nil {
		return nil, fmt.Errorf("wire: create roles handler: %w", err)
	}

	// 5 endpoints (3 business-scoped, 1 auth-only token, 1 public token).
	// + svcs.AuditLogger for rbac.invitation_* events.
	invitationsHandler, err := handler.NewInvitationsHandler(
		repos.Invitation,
		repos.BusinessMembership,
		repos.Role,
		repos.User,
		repos.Business,
		h.PG,
		svcs.AuthzCache,
		svcs.AuditLogger,
	)
	if err != nil {
		return nil, fmt.Errorf("wire: create invitations handler: %w", err)
	}

	// repos.AuditLog is the domain interface (Insert/ListByBusiness/
	// DeleteOlderThan) which does NOT include ListByBusinessWithActors.
	// Checked type-assert — panic at boot on wiring drift rather than 500
	// silently at request time.
	auditLister, ok := repos.AuditLog.(handler.AuditLogLister)
	if !ok {
		return nil, fmt.Errorf("wire: AuditLog repo does not satisfy handler.AuditLogLister (impl drift)")
	}
	auditLogHandler := handler.NewAuditLogHandler(auditLister)

	// cfg.CORSAllowedOrigins backs the Restore endpoint's Origin-header CSRF
	// check. Nil-typed when svcs.AccountDeletion is nil so route registration
	// can skip.
	var userDeletionHandler *handler.UserDeletionHandler
	if svcs.AccountDeletion != nil {
		userDeletionHandler = handler.NewUserDeletionHandler(svcs.AccountDeletion, cfg.CORSAllowedOrigins)
	}

	// cfg.CORSAllowedOrigins backs the Origin-header CSRF check on the two
	// write endpoints (/auth/consents + /users/me/consents/pdn/withdraw).
	var consentsHandler *handler.ConsentsHandler
	if svcs.Consent != nil {
		consentsHandler = handler.NewConsentsHandler(svcs.Consent, repos.UserConsents, cfg.CORSAllowedOrigins)
	}
	// /auth/me populates requiresReconsent via the ConsentDiffer
	if svcs.Consent != nil {
		authHandler.SetConsentDiffer(svcs.Consent)
	}

	return &router.Handlers{
		Auth:            authHandler,
		Business:        businessHandler,
		Integration:     integrationHandler,
		Conversation:    conversationHandler,
		OAuth:           oauthHandler,
		Connect:         connectHandler,
		InternalToken:   internalTokenHandler,
		InternalBilling: internalBillingHandler,
		ChatProxy:       chatProxyHandler,
		Review:          reviewHandler,
		Post:            postHandler,
		AgentTask:       agentTaskHandler,
		Project:         projectHandler,
		HITL:            hitlHandler,
		Titler:          titlerHandler,
		Search:          searchHandler,
		Platforms:       platformsHandler,
		Permissions:     handler.NewPermissionsHandler(),
		Members:         membersHandler,
		Roles:           rolesHandler,
		Invitations:     invitationsHandler,
		AuditLog:        auditLogHandler,
		UserDeletion:    userDeletionHandler,
		Consents:        consentsHandler,
		Telemetry:       &handler.TelemetryHandler{}, // zero-dep
	}, nil
}
