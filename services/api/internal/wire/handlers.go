package wire

import (
	"fmt"
	"net/http"
	"time"

	"github.com/f1xgun/onevoice/services/api/internal/config"
	"github.com/f1xgun/onevoice/services/api/internal/handler"
	"github.com/f1xgun/onevoice/services/api/internal/handler/connect"
	"github.com/f1xgun/onevoice/services/api/internal/handler/oauth"
	"github.com/f1xgun/onevoice/services/api/internal/router"
)

// Handlers constructs every HTTP handler used by the API service and
// returns them aggregated in *router.Handlers ready for router.Setup.
//
// Each handler constructor signature is locked by the existing handler
// package — this function is wiring only, no business logic.
//
// Phase 19 / Plan 19-04 split: OAuthHandler (true OAuth code-flow) lives
// in handler/oauth; ConnectHandler (paste-flow) lives in handler/connect.
// Both are constructed here.
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

	// Plan 19-04: paste-flow handler (Telegram + VK community access token).
	// Narrow ConnectConfig per RESEARCH §16 Q3 — paste-flow doesn't need
	// the OAuth client credentials.
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

	authHandler, err := handler.NewAuthHandler(svcs.User, cfg.SecureCookies)
	if err != nil {
		return nil, fmt.Errorf("wire: create auth handler: %w", err)
	}
	businessHandler, err := handler.NewBusinessHandler(svcs.Business, svcs.PlatformSync, svcs.ObjectStorage)
	if err != nil {
		return nil, fmt.Errorf("wire: create business handler: %w", err)
	}
	integrationHandler, err := handler.NewIntegrationHandler(svcs.Integration, svcs.Business)
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

	// Phase 15 Projects — three-line wiring through the project service
	// already constructed in wire.Services.
	projectHandler, err := handler.NewProjectHandler(svcs.Project, svcs.Business)
	if err != nil {
		return nil, fmt.Errorf("wire: create project handler: %w", err)
	}

	// Conversation handler depends on business + project services for Phase 15
	// create-conversation scoping and the /move endpoint system-note append.
	conversationHandler, err := handler.NewConversationHandler(repos.Conversation, repos.Message, svcs.Business, svcs.Project, h.PendingToolCallRepo)
	if err != nil {
		return nil, fmt.Errorf("wire: create conversation handler: %w", err)
	}

	// Chat proxy enriches each /chat/{id} request with the conversation's
	// project_* fields (PROJ-09 layering) — requires projectService and
	// conversationRepo per Plan 15-04 Task 3.
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
		cfg.OrchestratorURL,
		nil,
		svcs.Titler, // Phase 18 Plan 05 — optional auto-titler; nil when titling is disabled.
	)

	hitlHandler, err := handler.NewHITLHandler(svcs.HITL, svcs.Business, repos.Conversation)
	if err != nil {
		return nil, fmt.Errorf("wire: create hitl handler: %w", err)
	}

	// Wire the shared ToolsRegistryCache into the business + project
	// handlers so PUT /business/{id}/tool-approvals and
	// PUT /projects/{id} can validate approval-overrides keys against the
	// live orchestrator registry before persisting (POLICY-05, POLICY-06).
	businessHandler.SetToolsCache(svcs.ToolsCache)
	projectHandler.SetToolsCache(svcs.ToolsCache)

	// Phase 18 Plan 05 — TitlerHandler for POST /conversations/{id}/regenerate-title.
	// titler may be nil here (graceful disable per A6); the handler returns 503
	// in that case. conversationRepo + messageRepo are required (panic-on-nil).
	titlerHandler := handler.NewTitlerHandler(svcs.Titler, repos.Conversation, repos.Message)

	// Phase 19 Plan 19-03 — search handler. Constructed with the searcher
	// (built in wire.Services; readiness flag already flipped) +
	// businessService for resolving the caller's businessID server-side
	// from the bearer's userID.
	searchHandler, err := handler.NewSearchHandler(svcs.Searcher, svcs.Business)
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

	return &router.Handlers{
		Auth:          authHandler,
		Business:      businessHandler,
		Integration:   integrationHandler,
		Conversation:  conversationHandler,
		OAuth:         oauthHandler,
		Connect:       connectHandler,
		InternalToken: internalTokenHandler,
		ChatProxy:     chatProxyHandler,
		Review:        reviewHandler,
		Post:          postHandler,
		AgentTask:     agentTaskHandler,
		Project:       projectHandler,
		HITL:          hitlHandler,
		Titler:        titlerHandler,
		Search:        searchHandler,
		Platforms:     platformsHandler,
	}, nil
}
