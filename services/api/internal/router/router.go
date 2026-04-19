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
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
)

// Handlers encapsulates all HTTP handlers
type Handlers struct {
	Auth          *handler.AuthHandler
	Business      *handler.BusinessHandler
	Integration   *handler.IntegrationHandler
	Conversation  *handler.ConversationHandler
	OAuth         *handler.OAuthHandler
	InternalToken *handler.InternalTokenHandler
	ChatProxy     *handler.ChatProxyHandler
	Review        *handler.ReviewHandler
	Post          *handler.PostHandler
	AgentTask     *handler.AgentTaskHandler
	Telemetry     *handler.TelemetryHandler
	Project       *handler.ProjectHandler
	HITL          *handler.HITLHandler // Phase 16: resolve + resume + GET /tools
}

// Setup creates and configures the Chi router with all routes and middleware
func Setup(handlers *Handlers, jwtSecret []byte, redisClient *redis.Client, hc *health.Checker) *chi.Mux {
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimiddleware.RequestID)
	r.Use(middleware.CorrelationID())
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:3000"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		ExposedHeaders:   []string{"Link", "X-Correlation-ID"},
		AllowCredentials: true,
		MaxAge:           300,
	}))
	r.Use(middleware.SecurityHeaders())
	r.Use(metrics.HTTPMiddleware)

	// API v1 routes
	r.Route("/api/v1", func(r chi.Router) {
		// Public routes (no auth)
		r.With(middleware.RateLimit(redisClient, 5, time.Minute)).Post("/auth/register", handlers.Auth.Register)
		r.With(middleware.RateLimit(redisClient, 10, time.Minute)).Post("/auth/login", handlers.Auth.Login)
		r.With(middleware.RateLimit(redisClient, 10, time.Minute)).Post("/auth/refresh", handlers.Auth.RefreshToken)

		// OAuth callback routes (public — state parameter validates session)
		r.Get("/oauth/vk/callback", handlers.OAuth.VKCallback)
		r.Get("/oauth/vk/community-callback", handlers.OAuth.VKCommunityCallback)
		r.Get("/oauth/yandex_business/callback", handlers.OAuth.YandexCallback)
		r.Get("/oauth/google_business/callback", handlers.OAuth.GoogleCallback)

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
			r.Put("/business/logo", handlers.Business.UploadLogo)

			// Integration routes
			r.Get("/integrations", handlers.Integration.ListIntegrations)
			r.Delete("/integrations/{id}", handlers.Integration.DeleteIntegration)

			// OAuth auth-url routes (need JWT to generate state with user context)
			r.Get("/integrations/vk/auth-url", handlers.OAuth.GetVKAuthURL)
			r.Get("/integrations/vk/communities", handlers.OAuth.VKCommunities)
			r.Get("/integrations/vk/community-auth-url", handlers.OAuth.VKCommunityAuthURL)
			r.Get("/integrations/yandex_business/auth-url", handlers.OAuth.GetYandexAuthURL)

			// VK community token route
			r.Post("/integrations/vk/connect", handlers.OAuth.ConnectVK)

			// Telegram routes
			r.Post("/integrations/telegram/verify", handlers.OAuth.VerifyTelegramLogin)
			r.Post("/integrations/telegram/connect", handlers.OAuth.ConnectTelegram)

			// Google Business routes
			r.Get("/integrations/google_business/auth-url", handlers.OAuth.GetGoogleAuthURL)
			r.Get("/integrations/google_business/locations", handlers.OAuth.GoogleLocations)
			r.Post("/integrations/google_business/select-location", handlers.OAuth.GoogleSelectLocation)

			// Chat proxy (replaces direct orchestrator access)
			r.With(middleware.RateLimitByUser(redisClient, 10, time.Minute)).Post("/chat/{conversationID}", handlers.ChatProxy.Chat)

			// Conversation routes
			r.Get("/conversations", handlers.Conversation.ListConversations)
			r.Post("/conversations", handlers.Conversation.CreateConversation)
			r.Get("/conversations/{id}", handlers.Conversation.GetConversation)
			r.Put("/conversations/{id}", handlers.Conversation.UpdateConversation)
			r.Delete("/conversations/{id}", handlers.Conversation.DeleteConversation)
			r.Get("/conversations/{id}/messages", handlers.Conversation.ListMessages)
			// Phase 15 (PROJ-06): move a chat between projects (or to "Без проекта")
			r.Post("/conversations/{id}/move", handlers.Conversation.MoveConversation)

			// Project routes (Phase 15 — projects foundation)
			r.Get("/projects", handlers.Project.List)
			r.Post("/projects", handlers.Project.Create)
			r.Get("/projects/{id}", handlers.Project.Get)
			r.Put("/projects/{id}", handlers.Project.Update)
			r.Delete("/projects/{id}", handlers.Project.Delete)
			r.Get("/projects/{id}/conversation-count", handlers.Project.ConversationCount)

			// Phase 16 HITL routes (Plan 16-07)
			if handlers.HITL != nil {
				r.Post("/conversations/{id}/pending-tool-calls/{batch_id}/resolve", handlers.HITL.ResolvePendingToolCalls)
				r.With(middleware.RateLimitByUser(redisClient, 10, time.Minute)).
					Post("/chat/{id}/resume", handlers.HITL.Resume)
				r.Get("/tools", handlers.HITL.GetTools)
			}
			// POLICY-05 business tool-approvals CRUD.
			if handlers.Business != nil {
				r.Get("/business/{id}/tool-approvals", handlers.Business.GetBusinessToolApprovals)
				r.Put("/business/{id}/tool-approvals", handlers.Business.UpdateBusinessToolApprovals)
			}

			// Password change
			r.Put("/auth/password", handlers.Auth.ChangePassword)

			// Review routes
			r.Get("/reviews", handlers.Review.ListReviews)
			r.Get("/reviews/{id}", handlers.Review.GetReview)
			r.Put("/reviews/{id}/reply", handlers.Review.ReplyToReview)

			// Post routes
			r.Get("/posts", handlers.Post.ListPosts)
			r.Get("/posts/{id}", handlers.Post.GetPost)

			// Agent task routes
			r.Get("/tasks", handlers.AgentTask.ListTasks)

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
