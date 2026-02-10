package router

import (
	"net/http"

	"github.com/f1xgun/onevoice/services/api/internal/handler"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/redis/go-redis/v9"
)

// Handlers encapsulates all HTTP handlers
type Handlers struct {
	Auth         *handler.AuthHandler
	Business     *handler.BusinessHandler
	Integration  *handler.IntegrationHandler
	Conversation *handler.ConversationHandler
}

// Setup creates and configures the Chi router with all routes and middleware
func Setup(handlers *Handlers, jwtSecret []byte, redisClient *redis.Client) *chi.Mux {
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

			// Integration routes
			r.Get("/integrations", handlers.Integration.ListIntegrations)
			r.Post("/integrations/{platform}/connect", handlers.Integration.ConnectIntegration)
			r.Delete("/integrations/{platform}", handlers.Integration.DeleteIntegration)

			// Conversation routes
			r.Get("/conversations", handlers.Conversation.ListConversations)
			r.Post("/conversations", handlers.Conversation.CreateConversation)
			r.Get("/conversations/{id}", handlers.Conversation.GetConversation)
		})
	})

	// Health check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	return r
}
