package wire

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/config"
	"github.com/f1xgun/onevoice/services/api/internal/handler"
	"github.com/f1xgun/onevoice/services/api/internal/handler/connect"
	"github.com/f1xgun/onevoice/services/api/internal/handler/oauth"
	"github.com/f1xgun/onevoice/services/api/internal/router"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// init wires the SoleOwnerExtractor hook so handler.UserDeletionHandler
// can return the 409 body with the businesses payload without importing
// the service package directly (which would force the service package
// to re-import a handler-side definition). The hook runs errors.As
// against *service.ErrSoleOwnerBusinesses and remaps to the public
// handler.SoleOwnerEntry shape.
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

// NewChatProxyHandler consumes the shared *orchestratorclient.Client built
// once in BuildServices (svcs.OrchClient) — there is no separate
// (orchestratorURL, httpClient) plumbing.

// Handlers constructs every HTTP handler used by the API service and
// returns them aggregated in *router.Handlers ready for router.Setup.
//
// Each handler constructor signature is locked by the existing handler
// package — this function is wiring only, no business logic.
//
// OAuthHandler (true OAuth code-flow) lives in handler/oauth; ConnectHandler
// (paste-flow) lives in handler/connect. Both are constructed here.
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

	// Paste-flow handler (Telegram + VK community access token).
	// Narrow ConnectConfig — paste-flow doesn't need the OAuth client
	// credentials.
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

	// 25a-04: internal billing write handler — POST
	// /internal/v1/billing/usage_logs. Plan 25a-02 wires repos.Billing
	// (Postgres-backed BillingRepository) which satisfies handler.BillingService
	// via the LogUsage method.
	internalBillingHandler := handler.NewInternalBillingHandler(repos.Billing, nil)

	// AuthHandler gains audit + jwtSecret args so it
	// can emit auth.* audit events and extract userID from refresh-token
	// claims before Redis invalidation during Logout.
	authHandler, err := handler.NewAuthHandler(svcs.User, cfg.SecureCookies, svcs.AuditLogger, []byte(cfg.JWTSecret))
	if err != nil {
		return nil, fmt.Errorf("wire: create auth handler: %w", err)
	}
	// inject password-reset service via setter to keep
	// NewAuthHandler's signature stable across the rest of the codebase.
	if svcs.PasswordReset != nil {
		authHandler.SetPasswordResetService(svcs.PasswordReset)
	}
	// inject email-verification service via setter.
	if svcs.EmailVerification != nil {
		authHandler.SetEmailVerificationService(svcs.EmailVerification)
	}
	// /auth/me must surface accountDeletion state
	// for soft-deleted users so they can render the grace banner +
	// click restore. Wire the deletion-aware GetByIDIncludingDeleted
	// pathway through the existing UserResetExt adapter.
	if repos.UserResetExt != nil {
		authHandler.SetMeUserExtraGetter(func(ctx context.Context, userID uuid.UUID) (*domain.User, error) {
			return repos.UserResetExt.GetByIDIncludingDeleted(ctx, userID)
		})
	}
	// Install lockout + SmartCaptcha verifier. Both dependencies are
	// nil-safe inside AuthHandler.Login. The router mounts
	// middleware.LockoutMiddleware on /auth/login using the same
	// svcs.Lockout instance.
	authHandler.WithLockout(svcs.Lockout, svcs.SmartCaptcha, cfg.SmartCaptchaFailOpen)
	businessHandler, err := handler.NewBusinessHandler(svcs.Business, svcs.PlatformSync, svcs.ObjectStorage)
	if err != nil {
		return nil, fmt.Errorf("wire: create business handler: %w", err)
	}
	// + svcs.AuditLogger for integration.disconnected
	// audit events from the handler-level Delete path.
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

	// Projects — three-line wiring through the project service already
	// constructed in wire.Services.
	projectHandler, err := handler.NewProjectHandler(svcs.Project)
	if err != nil {
		return nil, fmt.Errorf("wire: create project handler: %w", err)
	}

	// Conversation handler depends on business + project services for
	// create-conversation scoping; the /move endpoint and GET /messages
	// view are owned by svcs.Conversation.
	conversationHandler, err := handler.NewConversationHandler(repos.Conversation, repos.Message, svcs.Business, svcs.Project, svcs.Conversation)
	if err != nil {
		return nil, fmt.Errorf("wire: create conversation handler: %w", err)
	}

	// Chat proxy enriches each /chat/{id} request with the conversation's
	// project_* fields — requires projectService and conversationRepo.
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
		svcs.OrchClient, // single shared orchestrator client.
		svcs.Titler,     // optional auto-titler; nil when titling is disabled.
	)

	hitlHandler, err := handler.NewHITLHandler(svcs.HITL, svcs.Business, repos.Conversation)
	if err != nil {
		return nil, fmt.Errorf("wire: create hitl handler: %w", err)
	}

	// Wire the shared ToolsRegistryCache into the business + project
	// handlers so PUT /business/{id}/tool-approvals and
	// PUT /projects/{id} can validate approval-overrides keys against the
	// live orchestrator registry before persisting.
	businessHandler.SetToolsCache(svcs.ToolsCache)
	projectHandler.SetToolsCache(svcs.ToolsCache)

	// TitlerHandler for POST /conversations/{id}/regenerate-title.
	// titler may be nil here (graceful disable); the handler returns 503
	// in that case. conversationRepo + messageRepo are required (panic-on-nil).
	titlerHandler := handler.NewTitlerHandler(svcs.Titler, repos.Conversation, repos.Message)

	// Search handler. Constructed with the searcher (built in wire.Services;
	// readiness flag already flipped). Business scoping comes from the
	// RequireBusinessAccess middleware via BusinessContext in handler.
	searchHandler, err := handler.NewSearchHandler(svcs.Searcher)
	if err != nil {
		return nil, fmt.Errorf("wire: create search handler: %w", err)
	}

	// Platform registry — drives the public GET /api/v1/platforms endpoint.
	// Availability is derived from cfg so platforms missing required
	// credentials are surfaced as oauth_not_configured to the frontend.
	platformsHandler := handler.NewPlatformsHandler(handler.PlatformAvailability{
		Telegram:       cfg.TelegramBotToken != "",
		VK:             cfg.VKClientID != "" && cfg.VKClientSecret != "",
		YandexBusiness: cfg.YandexClientID != "" && cfg.YandexClientSecret != "",
		GoogleBusiness: cfg.GoogleClientID != "" && cfg.GoogleClientSecret != "",
	})

	// v2.0 RBAC handlers — members, roles, permissions registry.
	// MembersHandler gains svcs.AuditLogger so
	// rbac.role_granted + rbac.member_removed audit events fire AFTER tx.Commit.
	membersHandler, err := handler.NewMembersHandler(repos.BusinessMembership, repos.Role, repos.User, h.PG, svcs.AuthzCache, svcs.AuditLogger)
	if err != nil {
		return nil, fmt.Errorf("wire: create members handler: %w", err)
	}
	// extended signature: + membership repo (Delete fanout target
	// lookup) + pool (RepeatableRead tx for Create/Update/Delete) + invalidator
	// (InvalidateRole AFTER commit + InvalidateMember fanout per reassigned user).
	// + svcs.AuditLogger for rbac.role_* audit events.
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

	// v2.0 RBAC: invitations handler — 5 endpoints (3 business-scoped,
	// 1 auth-only token, 1 public token). See plan 03-04 / 03-05.
	// + svcs.AuditLogger for rbac.invitation_* audit events.
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

	// audit-log read handler. repos.AuditLog is
	// typed as the domain interface (Insert/ListByBusiness/DeleteOlderThan)
	// which does NOT include ListByBusinessWithActors — that method
	// returns a repository-package AuditLogRow type that intentionally
	// stays out of the domain layer. Type-assert here to bridge: the
	// underlying value IS the concrete *auditLogRepository, which
	// satisfies handler.AuditLogLister. The assertion is checked (panic
	// at boot if Repos ever swaps in a non-concrete impl) rather than the
	// silent comma-ok form because a missing reader at request time would
	// be a 500 with no telemetry — the boot panic surfaces wiring drift
	// loud and early.
	auditLister, ok := repos.AuditLog.(handler.AuditLogLister)
	if !ok {
		return nil, fmt.Errorf("wire: AuditLog repo does not satisfy handler.AuditLogLister (impl drift)")
	}
	auditLogHandler := handler.NewAuditLogHandler(auditLister)

	// DELETE /users/me + POST /users/me/restore.
	// The handler needs the AccountDeletionService + the CORS-allowed
	// origins for the Restore endpoint's Origin-header CSRF check
	// (T-DEL-10). When svcs.AccountDeletion is nil (legacy/test deploys),
	// we register a nil pointer so route registration knows to skip.
	var userDeletionHandler *handler.UserDeletionHandler
	if svcs.AccountDeletion != nil {
		userDeletionHandler = handler.NewUserDeletionHandler(svcs.AccountDeletion, cfg.CORSAllowedOrigins)
	}

	// /auth/consents + /users/me/consents +
	// /users/me/consents/pdn/withdraw. The handler needs the
	// ConsentService for write paths + the UserConsents repo for the
	// GET list path. CORS-allowed origins back the Origin-header
	// CSRF check on the two write endpoints.
	var consentsHandler *handler.ConsentsHandler
	if svcs.Consent != nil {
		consentsHandler = handler.NewConsentsHandler(svcs.Consent, repos.UserConsents, cfg.CORSAllowedOrigins)
	}
	// inject the ConsentDiffer into the auth handler so /auth/me
	// populates requiresReconsent. Always wired when svcs.Consent is set.
	if svcs.Consent != nil {
		authHandler.SetConsentDiffer(svcs.Consent)
	}

	return &router.Handlers{
		Auth:          authHandler,
		Business:      businessHandler,
		Integration:   integrationHandler,
		Conversation:  conversationHandler,
		OAuth:         oauthHandler,
		Connect:       connectHandler,
		InternalToken: internalTokenHandler,
		// 25a-04: internal billing write path.
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
		// v2.0 RBAC (AUTHZ-01): static permission registry endpoint.
		Permissions: handler.NewPermissionsHandler(),
		// v2.0 RBAC: member + role management.
		Members: membersHandler,
		Roles:   rolesHandler,
		// v2.0 RBAC: invitation lifecycle (Create/ListPending/Revoke/Preview/Accept).
		Invitations: invitationsHandler,
		// audit-log read handler. Bound to
		// GET /businesses/{id}/audit-logs via router.Setup.
		AuditLog: auditLogHandler,
		// DELETE /users/me + POST /users/me/restore.
		UserDeletion: userDeletionHandler,
		// /auth/consents + /users/me/consents +../pdn/withdraw.
		Consents: consentsHandler,
		// Telemetry handler is zero-dep; constructed inline.
		Telemetry: &handler.TelemetryHandler{},
	}, nil
}
