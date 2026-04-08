package router

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/redis/go-redis/v9"

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
}

// Setup creates and configures the Chi router with all routes and middleware
func Setup(handlers *Handlers, jwtSecret []byte, redisClient *redis.Client, uploadDir string) *chi.Mux {
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:3000"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// API v1 routes
	r.Route("/api/v1", func(r chi.Router) {
		// Public routes (no auth)
		r.Post("/auth/register", handlers.Auth.Register)
		r.Post("/auth/login", handlers.Auth.Login)
		r.Post("/auth/refresh", handlers.Auth.RefreshToken)

		// OAuth callback routes (public — state parameter validates session)
		r.Get("/oauth/vk/callback", handlers.OAuth.VKCallback)
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
			r.Get("/integrations/yandex_business/auth-url", handlers.OAuth.GetYandexAuthURL)

			// Telegram routes
			r.Post("/integrations/telegram/verify", handlers.OAuth.VerifyTelegramLogin)
			r.Post("/integrations/telegram/connect", handlers.OAuth.ConnectTelegram)

			// Google Business routes
			r.Get("/integrations/google_business/auth-url", handlers.OAuth.GetGoogleAuthURL)
			r.Get("/integrations/google_business/locations", handlers.OAuth.GoogleLocations)
			r.Post("/integrations/google_business/select-location", handlers.OAuth.GoogleSelectLocation)

			// Chat proxy (replaces direct orchestrator access)
			r.Post("/chat/{conversationID}", handlers.ChatProxy.Chat)

			// Conversation routes
			r.Get("/conversations", handlers.Conversation.ListConversations)
			r.Post("/conversations", handlers.Conversation.CreateConversation)
			r.Get("/conversations/{id}", handlers.Conversation.GetConversation)
			r.Put("/conversations/{id}", handlers.Conversation.UpdateConversation)
			r.Delete("/conversations/{id}", handlers.Conversation.DeleteConversation)
			r.Get("/conversations/{id}/messages", handlers.Conversation.ListMessages)

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
		})
	})

	// Serve uploaded logo files
	r.Handle("/uploads/*", http.StripPrefix("/uploads/", http.FileServer(http.Dir(uploadDir))))

	// Health check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	return r
}

// SetupInternal creates the internal mTLS-protected router.
func SetupInternal(handlers *Handlers) *chi.Mux {
	r := chi.NewRouter()
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)

	r.Get("/internal/v1/tokens", handlers.InternalToken.GetToken)
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	return r
}
