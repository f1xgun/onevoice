package router

import (
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/health"
	"github.com/f1xgun/onevoice/pkg/metrics"
	"github.com/f1xgun/onevoice/services/api/internal/handler"
	"github.com/f1xgun/onevoice/services/api/internal/handler/connect"
	"github.com/f1xgun/onevoice/services/api/internal/handler/oauth"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
)

// Per-endpoint per-window rate limits (window is always time.Minute today).
// Register is tighter than login because automated signup abuse is the
// primary cost of getting these wrong; login/refresh are looser to absorb
// retry storms after backend hiccups. Chat / HITL share a budget because
// the chat-proxy fans out into HITL resolve calls under the same auth.
//
// Defaults are surfaced as env vars (RATE_LIMIT_REGISTER, RATE_LIMIT_LOGIN,
// RATE_LIMIT_CHAT, RATE_LIMIT_HITL); the caller of Setup passes a
// RateLimits struct constructed from those values.
const corsMaxAge = 300

// RateLimits aggregates the per-endpoint per-minute request budgets so
// router.Setup takes one parameter instead of four.
type RateLimits struct {
	Register int
	Login    int
	Chat     int
	HITL     int
}

// Handlers encapsulates all HTTP handlers
type Handlers struct {
	Auth          *handler.AuthHandler
	Business      *handler.BusinessHandler
	Integration   *handler.IntegrationHandler
	Conversation  *handler.ConversationHandler
	OAuth         *oauth.OAuthHandler     // true OAuth code-flow (VK / Yandex / Google)
	Connect       *connect.ConnectHandler // paste-flow integrations (Telegram bot-token, VK community access token)
	InternalToken *handler.InternalTokenHandler
	ChatProxy     *handler.ChatProxyHandler
	Review        *handler.ReviewHandler
	Post          *handler.PostHandler
	AgentTask     *handler.AgentTaskHandler
	Telemetry     *handler.TelemetryHandler
	Project       *handler.ProjectHandler
	HITL          *handler.HITLHandler        // resolve + resume + GET /tools
	Titler        *handler.TitlerHandler      // POST /conversations/{id}/regenerate-title
	Search        *handler.SearchHandler      // GET /api/v1/search
	Platforms     *handler.PlatformsHandler   // Public platform registry
	Permissions   *handler.PermissionsHandler // RBAC: static permission registry
	Members       *handler.MembersHandler     // RBAC: member management
	Roles         *handler.RolesHandler       // RBAC: role listing
}

// Setup creates and configures the Chi router with all routes and middleware.
// allowedOrigins is the CORS whitelist sourced from CORS_ALLOWED_ORIGINS;
// callers MUST supply at least one entry so the public frontend origin can be
// configured per-environment without rebuilding the binary.
// rateLimits controls per-endpoint per-minute request budgets — see the
// RateLimits comment block above for the role of each field.
// authzCache backs the RequireBusinessAccess middleware that gates the
// /businesses/{id}/... subtree (Phase 2 v2.0 RBAC).
func Setup(handlers *Handlers, jwtSecret []byte, redisClient *redis.Client, hc *health.Checker, allowedOrigins []string, rateLimits RateLimits, authzCache *authz.Cache) *chi.Mux {
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimiddleware.RequestID)
	r.Use(middleware.CorrelationID())
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		ExposedHeaders:   []string{"Link", "X-Correlation-ID"},
		AllowCredentials: true,
		MaxAge:           corsMaxAge,
	}))
	r.Use(middleware.SecurityHeaders())
	r.Use(metrics.HTTPMiddleware)

	// API v1 routes
	r.Route("/api/v1", func(r chi.Router) {
		// Public routes (no auth)
		r.With(middleware.RateLimit(redisClient, rateLimits.Register, time.Minute)).Post("/auth/register", handlers.Auth.Register)
		r.With(middleware.RateLimit(redisClient, rateLimits.Login, time.Minute)).Post("/auth/login", handlers.Auth.Login)
		r.With(middleware.RateLimit(redisClient, rateLimits.Login, time.Minute)).Post("/auth/refresh", handlers.Auth.RefreshToken)

		// OAuth callback routes (public — state parameter validates session)
		r.Get("/oauth/vk/callback", handlers.OAuth.VKCallback)
		r.Get("/oauth/vk/community-callback", handlers.OAuth.VKCommunityCallback)
		r.Get("/oauth/yandex_business/callback", handlers.OAuth.YandexCallback)
		r.Get("/oauth/google_business/callback", handlers.OAuth.GoogleCallback)

		// Platform registry — single source of truth for the integration list.
		// Public so the marketing landing page can render it without auth.
		// Returns only non-sensitive metadata (name, description, status).
		r.Get("/platforms", handlers.Platforms.List)

		// Protected routes (require auth)
		r.Group(func(r chi.Router) {
			// Auth middleware
			r.Use(middleware.Auth(jwtSecret))

			// Auth-only routes (not business-scoped).
			r.Post("/auth/logout", handlers.Auth.Logout)
			r.Get("/auth/me", handlers.Auth.Me)
			r.Put("/auth/password", handlers.Auth.ChangePassword)

			// Phase 1 v2.0 RBAC (AUTHZ-01 / CONTEXT D-15): static permission registry.
			// Auth-required (any logged-in user) — no business scope.
			if handlers.Permissions != nil {
				r.Get("/permissions", handlers.Permissions.List)
			}

			// BIZ-02 + BIZ-03 (auth-only, NOT business-scoped).
			r.Get("/businesses", handlers.Business.ListUserBusinesses)
			r.Post("/businesses", handlers.Business.CreateBusiness)

			// Phase 19 manual review refresh — kicks the cross-business sync;
			// no business scope so any authed user can trigger it.
			r.Post("/reviews/refresh", handlers.Review.RefreshReviews)

			r.Post("/telemetry", handlers.Telemetry.Ingest)

			// Business-scoped subtree — single chokepoint via RequireBusinessAccess.
			// Every route under /businesses/{id}/... is gated by this middleware, which:
			//   - parses and validates the business UUID from the URL
			//   - looks up membership (404 on non-member, not 403 — AUTHZ-05)
			//   - injects BusinessContext into ctx with the caller's role + permissions
			r.Route("/businesses/{id}", func(r chi.Router) {
				r.Use(authz.RequireBusinessAccess(authzCache, middleware.GetUserID))

				// Business profile.
				r.Get("/", handlers.Business.GetBusiness)
				r.Put("/", handlers.Business.UpdateBusiness)
				r.Put("/schedule", handlers.Business.UpdateSchedule)
				r.Put("/voice-tone", handlers.Business.UpdateVoiceTone)
				r.Put("/logo", handlers.Business.UploadLogo)
				r.Get("/tool-approvals", handlers.Business.GetBusinessToolApprovals)
				r.Put("/tool-approvals", handlers.Business.UpdateBusinessToolApprovals)

				// Integrations.
				r.Get("/integrations", handlers.Integration.ListIntegrations)
				r.Delete("/integrations/{integrationId}", handlers.Integration.DeleteIntegration)

				// OAuth auth-url routes (need JWT + business context to generate state)
				r.Get("/integrations/vk/auth-url", handlers.OAuth.GetVKAuthURL)
				r.Get("/integrations/vk/communities", handlers.OAuth.VKCommunities)
				r.Get("/integrations/vk/community-auth-url", handlers.OAuth.VKCommunityAuthURL)
				r.Post("/integrations/vk/connect", handlers.Connect.ConnectVK)
				r.Post("/integrations/vk/{id}/refresh-name", handlers.Connect.RefreshVKCommunityName)

				r.Get("/integrations/yandex_business/auth-url", handlers.OAuth.GetYandexAuthURL)
				// Yandex.Business cookie-paste flow (replaces the broken OAuth-only path:
				// Yandex doesn't expose a Sprav API for the actions we automate, so the
				// Playwright agent needs real browser session cookies. See AGENTS.md +
				// memory/project_yandex_business_no_oauth_api.md for the full rationale.)
				r.Post("/integrations/yandex_business/probe", handlers.OAuth.ProbeYandexBusiness)
				r.Post("/integrations/yandex_business/companies", handlers.OAuth.ListYandexCompanies)
				r.Post("/integrations/yandex_business/connect", handlers.OAuth.ConnectYandexBusiness)
				r.Post("/integrations/yandex_business/{id}/refresh-name", handlers.OAuth.RefreshYandexBusinessName)

				r.Get("/integrations/google_business/auth-url", handlers.OAuth.GetGoogleAuthURL)
				r.Get("/integrations/google_business/locations", handlers.OAuth.GoogleLocations)
				r.Post("/integrations/google_business/select-location", handlers.OAuth.GoogleSelectLocation)

				r.Post("/integrations/telegram/verify", handlers.Connect.VerifyTelegramLogin)
				r.Post("/integrations/telegram/connect", handlers.Connect.ConnectTelegram)
				r.Post("/integrations/telegram/refresh", handlers.Connect.RefreshTelegramLinkedGroup)

				// Chat (rate-limited via env-tunable RateLimits.Chat).
				r.With(middleware.RateLimitByUser(redisClient, rateLimits.Chat, time.Minute)).
					Post("/chat/{conversationID}", handlers.ChatProxy.Chat)
				if handlers.HITL != nil {
					r.With(middleware.RateLimitByUser(redisClient, rateLimits.HITL, time.Minute)).
						Post("/chat/{id}/resume", handlers.HITL.Resume)
					r.Post("/conversations/{id}/pending-tool-calls/{batch_id}/resolve", handlers.HITL.ResolvePendingToolCalls)
					r.Get("/tools", handlers.HITL.GetTools)
				}

				// Conversations.
				r.Get("/conversations", handlers.Conversation.ListConversations)
				r.Post("/conversations", handlers.Conversation.CreateConversation)
				r.Get("/conversations/{id}", handlers.Conversation.GetConversation)
				r.Put("/conversations/{id}", handlers.Conversation.UpdateConversation)
				r.Delete("/conversations/{id}", handlers.Conversation.DeleteConversation)
				r.Get("/conversations/{id}/messages", handlers.Conversation.ListMessages)
				r.Post("/conversations/{id}/move", handlers.Conversation.MoveConversation)
				// Phase 19 (UI-03 / D-02): pin / unpin a conversation.
				r.Post("/conversations/{id}/pin", handlers.Conversation.Pin)
				r.Post("/conversations/{id}/unpin", handlers.Conversation.Unpin)
				// Phase 18 / TITLE-09: regenerate the auto-title for an existing chat.
				if handlers.Titler != nil {
					r.Post("/conversations/{id}/regenerate-title", handlers.Titler.RegenerateTitle)
				}

				// Projects (Phase 15).
				r.Get("/projects", handlers.Project.List)
				r.Post("/projects", handlers.Project.Create)
				r.Get("/projects/{id}", handlers.Project.Get)
				r.Put("/projects/{id}", handlers.Project.Update)
				r.Delete("/projects/{id}", handlers.Project.Delete)
				r.Get("/projects/{id}/conversation-count", handlers.Project.ConversationCount)

				// Search.
				// Phase 19 / Plan 19-03 / SEARCH-02: sidebar search.
				if handlers.Search != nil {
					r.Get("/search", handlers.Search.Search)
				}

				// Review routes.
				r.Get("/reviews", handlers.Review.ListReviews)
				r.Get("/reviews/{id}", handlers.Review.GetReview)
				r.Put("/reviews/{id}/reply", handlers.Review.ReplyToReview)

				// Post routes.
				r.Get("/posts", handlers.Post.ListPosts)
				r.Get("/posts/{id}", handlers.Post.GetPost)

				// Agent task routes.
				r.Get("/tasks", handlers.AgentTask.ListTasks)
				r.Get("/tasks/stream", handlers.AgentTask.StreamTasks)

				// Members + roles (NEW — Phase 2 v2.0 RBAC).
				if handlers.Members != nil {
					r.Get("/members", handlers.Members.ListMembers)
					r.Patch("/members/{userId}", handlers.Members.UpdateMemberRole)
					r.Delete("/members/{userId}", handlers.Members.RemoveMember)
				}
				if handlers.Roles != nil {
					r.Get("/roles", handlers.Roles.List)
				}
			})
		})

		// Platform registry — public endpoint outside the auth group so the
		// marketing landing page can render without a token. Returns only
		// non-sensitive metadata (name, description, configured/availability).
		if handlers.Platforms != nil {
			r.Get("/platforms", handlers.Platforms.List)
		}
	})

	// Prometheus metrics
	r.Handle("/metrics", promhttp.Handler())

	// Health check
	r.Get("/health/live", hc.LiveHandler())
	r.Get("/health/ready", hc.ReadyHandler())
	r.Get("/health", hc.LiveHandler()) // backward compat

	return r
}

// SetupInternal creates the internal mTLS-protected router.
func SetupInternal(handlers *Handlers, hc *health.Checker) *chi.Mux {
	r := chi.NewRouter()
	r.Use(chimiddleware.RequestID)
	r.Use(middleware.CorrelationID())
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)

	r.Get("/internal/v1/tokens", handlers.InternalToken.GetToken)
	r.Get("/health/live", hc.LiveHandler())
	r.Get("/health/ready", hc.ReadyHandler())
	r.Get("/health", hc.LiveHandler()) // backward compat

	return r
}
