package router

import (
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"

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
	HITL          *handler.HITLHandler   // resolve + resume + GET /tools
	Titler        *handler.TitlerHandler // POST /conversations/{id}/regenerate-title
	Search        *handler.SearchHandler // GET /api/v1/search
	Platforms     *handler.PlatformsHandler
}

// Setup creates and configures the Chi router with all routes and middleware.
// allowedOrigins is the CORS whitelist sourced from CORS_ALLOWED_ORIGINS;
// callers MUST supply at least one entry so the public frontend origin can be
// configured per-environment without rebuilding the binary.
// rateLimits controls per-endpoint per-minute request budgets — see the
// RateLimits comment block above for the role of each field.
func Setup(handlers *Handlers, jwtSecret []byte, redisClient *redis.Client, hc *health.Checker, allowedOrigins []string, rateLimits RateLimits) *chi.Mux {
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

			// Auth routes
			r.Post("/auth/logout", handlers.Auth.Logout)
			r.Get("/auth/me", handlers.Auth.Me)

			// Business routes
			r.Get("/business", handlers.Business.GetBusiness)
			r.Put("/business", handlers.Business.UpdateBusiness)
			r.Put("/business/schedule", handlers.Business.UpdateSchedule)
			r.Put("/business/voice-tone", handlers.Business.UpdateVoiceTone)
			r.Put("/business/logo", handlers.Business.UploadLogo)

			// Integration routes
			r.Get("/integrations", handlers.Integration.ListIntegrations)
			r.Delete("/integrations/{id}", handlers.Integration.DeleteIntegration)

			// OAuth auth-url routes (need JWT to generate state with user context)
			r.Get("/integrations/vk/auth-url", handlers.OAuth.GetVKAuthURL)
			r.Get("/integrations/vk/communities", handlers.OAuth.VKCommunities)
			r.Get("/integrations/vk/community-auth-url", handlers.OAuth.VKCommunityAuthURL)
			r.Get("/integrations/yandex_business/auth-url", handlers.OAuth.GetYandexAuthURL)

			// Yandex.Business cookie-paste flow (replaces the broken OAuth-only path:
			// Yandex doesn't expose a Sprav API for the actions we automate, so the
			// Playwright agent needs real browser session cookies. See AGENTS.md +
			// memory/project_yandex_business_no_oauth_api.md for the full rationale.)
			r.Post("/integrations/yandex_business/probe", handlers.OAuth.ProbeYandexBusiness)
			// Synchronous Sprav company picker — RPA-driven, ~25–45s.
			r.Post("/integrations/yandex_business/companies", handlers.OAuth.ListYandexCompanies)
			r.Post("/integrations/yandex_business/connect", handlers.OAuth.ConnectYandexBusiness)
			// Lazy backfill of the Sprav permalink + business name via agent.
			r.Post("/integrations/yandex_business/{id}/refresh-name", handlers.OAuth.RefreshYandexBusinessName)

			// VK community token route
			r.Post("/integrations/vk/connect", handlers.Connect.ConnectVK)
			// Lazy backfill of missing community names on existing integrations.
			r.Post("/integrations/vk/{id}/refresh-name", handlers.Connect.RefreshVKCommunityName)

			// Telegram routes
			r.Post("/integrations/telegram/verify", handlers.Connect.VerifyTelegramLogin)
			r.Post("/integrations/telegram/connect", handlers.Connect.ConnectTelegram)
			r.Post("/integrations/telegram/refresh", handlers.Connect.RefreshTelegramLinkedGroup)

			// Google Business routes
			r.Get("/integrations/google_business/auth-url", handlers.OAuth.GetGoogleAuthURL)
			r.Get("/integrations/google_business/locations", handlers.OAuth.GoogleLocations)
			r.Post("/integrations/google_business/select-location", handlers.OAuth.GoogleSelectLocation)

			// Chat proxy (replaces direct orchestrator access)
			r.With(middleware.RateLimitByUser(redisClient, rateLimits.Chat, time.Minute)).Post("/chat/{conversationID}", handlers.ChatProxy.Chat)

			// Conversation routes
			r.Get("/conversations", handlers.Conversation.ListConversations)
			r.Post("/conversations", handlers.Conversation.CreateConversation)
			r.Get("/conversations/{id}", handlers.Conversation.GetConversation)
			r.Put("/conversations/{id}", handlers.Conversation.UpdateConversation)
			r.Delete("/conversations/{id}", handlers.Conversation.DeleteConversation)
			r.Get("/conversations/{id}/messages", handlers.Conversation.ListMessages)
			// move a chat between projects (or to "Без проекта")
			r.Post("/conversations/{id}/move", handlers.Conversation.MoveConversation)
			// pin / unpin a conversation. Atomic
			// repo writes scoped by (id, business_id, user_id) defend
			// against cross-tenant pin manipulation.
			r.Post("/conversations/{id}/pin", handlers.Conversation.Pin)
			r.Post("/conversations/{id}/unpin", handlers.Conversation.Unpin)
			// regenerate the auto-title for an existing chat.
			if handlers.Titler != nil {
				r.Post("/conversations/{id}/regenerate-title", handlers.Titler.RegenerateTitle)
			}

			// sidebar search.
			// Mongo $text against conversations.title + messages.content,
			// scoped by (business_id, user_id, project_id?). Returns 503 +
			// Retry-After: 5 until EnsureSearchIndexes completes at startup
			// (readiness flag flips after the
			// happens-before edge in service.Searcher.MarkIndexesReady).
			if handlers.Search != nil {
				r.Get("/search", handlers.Search.Search)
			}

			// Project routes
			r.Get("/projects", handlers.Project.List)
			r.Post("/projects", handlers.Project.Create)
			r.Get("/projects/{id}", handlers.Project.Get)
			r.Put("/projects/{id}", handlers.Project.Update)
			r.Delete("/projects/{id}", handlers.Project.Delete)
			r.Get("/projects/{id}/conversation-count", handlers.Project.ConversationCount)

			// HITL routes
			if handlers.HITL != nil {
				r.Post("/conversations/{id}/pending-tool-calls/{batch_id}/resolve", handlers.HITL.ResolvePendingToolCalls)
				r.With(middleware.RateLimitByUser(redisClient, rateLimits.HITL, time.Minute)).
					Post("/chat/{id}/resume", handlers.HITL.Resume)
				r.Get("/tools", handlers.HITL.GetTools)
			}
			// business tool-approvals CRUD.
			if handlers.Business != nil {
				r.Get("/business/{id}/tool-approvals", handlers.Business.GetBusinessToolApprovals)
				r.Put("/business/{id}/tool-approvals", handlers.Business.UpdateBusinessToolApprovals)
			}

			// Password change
			r.Put("/auth/password", handlers.Auth.ChangePassword)

			// Review routes
			r.Get("/reviews", handlers.Review.ListReviews)
			r.Post("/reviews/refresh", handlers.Review.RefreshReviews)
			r.Get("/reviews/{id}", handlers.Review.GetReview)
			r.Put("/reviews/{id}/reply", handlers.Review.ReplyToReview)

			// Post routes
			r.Get("/posts", handlers.Post.ListPosts)
			r.Get("/posts/{id}", handlers.Post.GetPost)

			// Agent task routes
			r.Get("/tasks", handlers.AgentTask.ListTasks)
			r.Get("/tasks/stream", handlers.AgentTask.StreamTasks)

			// Telemetry
			r.Post("/telemetry", handlers.Telemetry.Ingest)
		})
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
