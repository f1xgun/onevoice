// Package router wires HTTP routes for the API service.
//
// See docs/api/routes.md for the route catalog, middleware stack, and
// security gating per route group.
package router

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/health"
	"github.com/f1xgun/onevoice/pkg/i18n"
	"github.com/f1xgun/onevoice/pkg/lockout"
	"github.com/f1xgun/onevoice/pkg/metrics"
	"github.com/f1xgun/onevoice/services/api/internal/handler"
	"github.com/f1xgun/onevoice/services/api/internal/handler/connect"
	"github.com/f1xgun/onevoice/services/api/internal/handler/oauth"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
)

// deletionGraceDaysForRouter mirrors service.AccountDeletionService.graceDays
// so BlockWritesDuringGrace can compute the 423 body's deletionDate.
const deletionGraceDaysForRouter = 30

// passThroughMiddleware is a no-op fallback used when a nil UserLookup is
// passed, keeping route declarations uniform.
func passThroughMiddleware(next http.Handler) http.Handler { return next }

// corsMaxAge is the CORS preflight cache lifetime in seconds.
const corsMaxAge = 300

// RateLimits aggregates the per-endpoint per-minute request budgets so
// router.Setup takes one parameter instead of four. See docs/api/routes.md
// for the budget rationale per field.
type RateLimits struct {
	Register int
	Login    int
	Chat     int
	HITL     int
	Consents int
}

// Handlers encapsulates all HTTP handlers consumed by Setup / SetupInternal.
type Handlers struct {
	Auth            *handler.AuthHandler
	Business        *handler.BusinessHandler
	Integration     *handler.IntegrationHandler
	Conversation    *handler.ConversationHandler
	OAuth           *oauth.OAuthHandler
	Connect         *connect.ConnectHandler
	InternalToken   *handler.InternalTokenHandler
	InternalBilling *handler.InternalBillingHandler // mTLS internal only
	ChatProxy       *handler.ChatProxyHandler
	Review          *handler.ReviewHandler
	Post            *handler.PostHandler
	AgentTask       *handler.AgentTaskHandler
	Telemetry       *handler.TelemetryHandler
	Project         *handler.ProjectHandler
	HITL            *handler.HITLHandler
	Titler          *handler.TitlerHandler
	Search          *handler.SearchHandler
	Platforms       *handler.PlatformsHandler
	Permissions     *handler.PermissionsHandler
	Members         *handler.MembersHandler
	Roles           *handler.RolesHandler
	Invitations     *handler.InvitationsHandler
	AuditLog        *handler.AuditLogHandler
	UserDeletion    *handler.UserDeletionHandler
	Consents        *handler.ConsentsHandler
}

// Setup creates and configures the public Chi router. See
// docs/api/routes.md for the full route catalog, middleware stack, and
// nil-tolerance contract on UserLookup / pgPool / lock.
func Setup(handlers *Handlers, jwtSecret []byte, redisClient *redis.Client, hc *health.Checker, allowedOrigins []string, rateLimits RateLimits, authzCache *authz.Cache, users middleware.UserLookup, pgPool *pgxpool.Pool, lock *lockout.Lockout) *chi.Mux {
	r := chi.NewRouter()

	r.Use(chimiddleware.RequestID)
	r.Use(middleware.CorrelationID())
	// chi RealIP intentionally omitted: it rewrites RemoteAddr from XFF with
	// no trust knob, defeating the TRUSTED_PROXY_CIDRS gate in
	// middleware.ClientIP.
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Accept-Language", "Authorization", "Content-Type"},
		ExposedHeaders:   []string{"Link", "X-Correlation-ID"},
		AllowCredentials: true,
		MaxAge:           corsMaxAge,
	}))
	// LocaleResolver must run before SecurityHeaders / metrics / auth so
	// unauthenticated error responses are localized off Accept-Language.
	r.Use(i18n.LocaleMiddleware)
	r.Use(middleware.SecurityHeaders())
	r.Use(metrics.HTTPMiddleware)

	r.Route("/api/v1", func(r chi.Router) {
		// public — no auth
		r.With(middleware.RateLimit(redisClient, rateLimits.Register, time.Minute)).Post("/auth/register", handlers.Auth.Register)
		if lock != nil {
			// lockout BEFORE rate-limit so a locked account returns 423 (with
			// retry_after_seconds), not 429.
			r.With(middleware.LockoutMiddleware(lock)).
				With(middleware.RateLimit(redisClient, rateLimits.Login, time.Minute)).
				Post("/auth/login", handlers.Auth.Login)
		} else {
			// graceful disable: no Redis at boot → rate-limit-only legacy path
			r.With(middleware.RateLimit(redisClient, rateLimits.Login, time.Minute)).Post("/auth/login", handlers.Auth.Login)
		}
		r.With(middleware.RateLimit(redisClient, rateLimits.Login, time.Minute)).Post("/auth/refresh", handlers.Auth.RefreshToken)

		// password reset — no chi RateLimit wrapper; service-layer per-email
		// limiter enforces timing-parity contract uniformly.
		r.Post("/auth/password-reset/request", handlers.Auth.RequestPasswordReset)
		// POST only — no GET handler; scanner-protection against link prefetch.
		r.Post("/auth/password-reset/confirm", handlers.Auth.ConfirmPasswordReset)

		// public — no JWT. Returns 204 with NO Set-Cookie. POST only —
		// scanner-protection against link prefetch.
		r.Post("/auth/verify-email/confirm", handlers.Auth.VerifyConfirm)

		// public — state parameter validates session
		r.Get("/oauth/vk/callback", handlers.OAuth.VKCallback)
		r.Get("/oauth/vk/community-callback", handlers.OAuth.VKCommunityCallback)
		r.Get("/oauth/yandex_business/callback", handlers.OAuth.YandexCallback)
		r.Get("/oauth/google_business/callback", handlers.OAuth.GoogleCallback)

		// public — returns only non-sensitive platform metadata
		r.Get("/platforms", handlers.Platforms.List)

		// public — token IS the auth. Rate-limited with Login budget.
		if handlers.Invitations != nil {
			r.With(middleware.RateLimit(redisClient, rateLimits.Login, time.Minute)).
				Get("/invitations/{token}", handlers.Invitations.Preview)
		}

		// auth-required, NEVER decorated by BlockWritesDuringGrace —
		// escape hatches + idempotent reads.
		r.Group(func(r chi.Router) {
			r.Use(middleware.Auth(jwtSecret))
			r.Post("/auth/logout", handlers.Auth.Logout)
			r.Get("/auth/me", handlers.Auth.Me)
			r.Post("/auth/verify-email/resend", handlers.Auth.VerifyResend)
			r.Patch("/auth/email-before-verify", handlers.Auth.EmailBeforeVerify)
			if handlers.UserDeletion != nil {
				r.Delete("/users/me", handlers.UserDeletion.Delete)
				r.Post("/users/me/restore", handlers.UserDeletion.Restore)
			}
			// consents: right-to-withdraw cannot be gated by verification or grace
			// per 152-ФЗ Art. 21.
			if handlers.Consents != nil {
				r.With(middleware.RateLimitByUser(redisClient, rateLimits.Consents, time.Minute, "consents")).
					Post("/auth/consents", handlers.Consents.Reconsent)
				r.Get("/users/me/consents", handlers.Consents.ListMine)
				r.With(middleware.RateLimitByUser(redisClient, rateLimits.Consents, time.Minute, "consents")).
					Post("/users/me/consents/pdn/withdraw", handlers.Consents.WithdrawPDN)
			}
		})

		// auth-required + write-gated by 30-day grace (GETs bypass)
		r.Group(func(r chi.Router) {
			r.Use(middleware.Auth(jwtSecret))
			if pgPool != nil {
				r.Use(middleware.BlockWritesDuringGrace(pgPool, deletionGraceDaysForRouter))
			}

			r.Put("/auth/password", handlers.Auth.ChangePassword)
			r.Patch("/auth/locale", handlers.Auth.UpdatePreferredLocale)

			// auth-only, no business scope
			if handlers.Permissions != nil {
				r.Get("/permissions", handlers.Permissions.List)
			}

			// auth-only, no business scope
			r.Get("/businesses", handlers.Business.ListUserBusinesses)
			// day-7 soft-restrict: unverified users get 7 days before
			// business creation is blocked.
			if users != nil {
				r.With(middleware.RequireVerifiedEmailDay7(users)).
					Post("/businesses", handlers.Business.CreateBusiness)
			} else {
				r.Post("/businesses", handlers.Business.CreateBusiness)
			}

			r.Post("/reviews/refresh", handlers.Review.RefreshReviews)

			r.Post("/telemetry", handlers.Telemetry.Ingest)

			// auth-required, NOT business-scoped — {token} targets the business.
			if handlers.Invitations != nil {
				r.With(middleware.RateLimitByUser(redisClient, rateLimits.Login, time.Minute, "invite_accept")).
					Post("/invitations/{token}/accept", handlers.Invitations.Accept)
			}

			// business-scoped subtree — single chokepoint via RequireBusinessAccess
			// (returns 404 on non-member, not 403).
			r.Route("/businesses/{id}", func(r chi.Router) {
				r.Use(authz.RequireBusinessAccess(authzCache, middleware.GetUserID))

				r.Get("/", handlers.Business.GetBusiness)
				r.Put("/", handlers.Business.UpdateBusiness)
				r.Put("/schedule", handlers.Business.UpdateSchedule)
				r.Put("/voice-tone", handlers.Business.UpdateVoiceTone)
				r.Put("/logo", handlers.Business.UploadLogo)
				r.Get("/tool-approvals", handlers.Business.GetBusinessToolApprovals)
				r.Put("/tool-approvals", handlers.Business.UpdateBusinessToolApprovals)

				// integrations GET endpoints — never verification-gated
				r.Get("/integrations", handlers.Integration.ListIntegrations)
				r.Delete("/integrations/{integrationId}", handlers.Integration.DeleteIntegration)

				// auth-url generators need JWT + business context for state
				r.Get("/integrations/vk/auth-url", handlers.OAuth.GetVKAuthURL)
				r.Get("/integrations/vk/communities", handlers.OAuth.VKCommunities)
				r.Get("/integrations/vk/community-auth-url", handlers.OAuth.VKCommunityAuthURL)
				r.Get("/integrations/yandex_business/auth-url", handlers.OAuth.GetYandexAuthURL)
				r.Get("/integrations/google_business/auth-url", handlers.OAuth.GetGoogleAuthURL)
				r.Get("/integrations/google_business/locations", handlers.OAuth.GoogleLocations)

				// day-0 hard-block on connect POSTs — attacker surface for spam
				// token connection. Wrapped individually so decorator order stays
				// explicit at the route declaration.
				integWith := func() func(http.Handler) http.Handler {
					if users == nil {
						return passThroughMiddleware
					}
					return middleware.RequireVerifiedEmailDay0(users)
				}()
				r.With(integWith).Post("/integrations/vk/connect", handlers.Connect.ConnectVK)
				r.With(integWith).Post("/integrations/vk/{id}/refresh-name", handlers.Connect.RefreshVKCommunityName)
				r.With(integWith).Post("/integrations/yandex_business/probe", handlers.OAuth.ProbeYandexBusiness)
				r.With(integWith).Post("/integrations/yandex_business/companies", handlers.OAuth.ListYandexCompanies)
				r.With(integWith).Post("/integrations/yandex_business/connect", handlers.OAuth.ConnectYandexBusiness)
				r.With(integWith).Post("/integrations/yandex_business/{id}/refresh-name", handlers.OAuth.RefreshYandexBusinessName)
				r.With(integWith).Post("/integrations/google_business/select-location", handlers.OAuth.GoogleSelectLocation)
				r.With(integWith).Post("/integrations/telegram/verify", handlers.Connect.VerifyTelegramLogin)
				r.With(integWith).Post("/integrations/telegram/connect", handlers.Connect.ConnectTelegram)
				r.With(integWith).Post("/integrations/telegram/refresh", handlers.Connect.RefreshTelegramLinkedGroup)

				// chat — rate-limit BEFORE verify so throttled requests
				// short-circuit before the DB lookup.
				if users != nil {
					r.With(
						middleware.RateLimitByUser(redisClient, rateLimits.Chat, time.Minute, "chat"),
						middleware.RequireVerifiedEmailDay7(users),
					).Post("/chat/{conversationID}", handlers.ChatProxy.Chat)
				} else {
					r.With(middleware.RateLimitByUser(redisClient, rateLimits.Chat, time.Minute, "chat")).
						Post("/chat/{conversationID}", handlers.ChatProxy.Chat)
				}
				if handlers.HITL != nil {
					r.With(middleware.RateLimitByUser(redisClient, rateLimits.HITL, time.Minute, "chat")).
						Post("/chat/{id}/resume", handlers.HITL.Resume)
					r.Post("/conversations/{id}/pending-tool-calls/{batch_id}/resolve", handlers.HITL.ResolvePendingToolCalls)
					r.Get("/tools", handlers.HITL.GetTools)
				}

				r.Get("/conversations", handlers.Conversation.ListConversations)
				r.Post("/conversations", handlers.Conversation.CreateConversation)
				r.Get("/conversations/{id}", handlers.Conversation.GetConversation)
				r.Put("/conversations/{id}", handlers.Conversation.UpdateConversation)
				r.Delete("/conversations/{id}", handlers.Conversation.DeleteConversation)
				r.Get("/conversations/{id}/messages", handlers.Conversation.ListMessages)
				r.Post("/conversations/{id}/move", handlers.Conversation.MoveConversation)
				r.Post("/conversations/{id}/pin", handlers.Conversation.Pin)
				r.Post("/conversations/{id}/unpin", handlers.Conversation.Unpin)
				if handlers.Titler != nil {
					r.Post("/conversations/{id}/regenerate-title", handlers.Titler.RegenerateTitle)
				}

				r.Get("/projects", handlers.Project.List)
				r.Post("/projects", handlers.Project.Create)
				r.Get("/projects/{id}", handlers.Project.Get)
				r.Put("/projects/{id}", handlers.Project.Update)
				r.Delete("/projects/{id}", handlers.Project.Delete)
				r.Get("/projects/{id}/conversation-count", handlers.Project.ConversationCount)

				if handlers.Search != nil {
					r.Get("/search", handlers.Search.Search)
				}

				r.Get("/reviews", handlers.Review.ListReviews)
				r.Get("/reviews/{id}", handlers.Review.GetReview)
				r.Put("/reviews/{id}/reply", handlers.Review.ReplyToReview)

				r.Get("/posts", handlers.Post.ListPosts)
				r.Get("/posts/{id}", handlers.Post.GetPost)

				r.Get("/tasks", handlers.AgentTask.ListTasks)
				r.Get("/tasks/stream", handlers.AgentTask.StreamTasks)

				if handlers.Members != nil {
					r.Get("/members", handlers.Members.ListMembers)
					r.Patch("/members/{userId}", handlers.Members.UpdateMemberRole)
					r.Delete("/members/{userId}", handlers.Members.RemoveMember)
				}
				if handlers.Roles != nil {
					r.Get("/roles", handlers.Roles.List)
					// role CRUD gated by RequireBusinessAccess (inherited) +
					// per-route Can check inside the handler.
					r.Post("/roles", handlers.Roles.Create)
					r.Patch("/roles/{roleId}", handlers.Roles.Update)
					r.Delete("/roles/{roleId}", handlers.Roles.Delete)
					// any member can read their own permissions
					r.Get("/me/permissions", handlers.Roles.MyPermissions)
				}

				if handlers.Invitations != nil {
					// POST gated by day-0 soft-restrict — spam vector
					if users != nil {
						r.With(middleware.RequireVerifiedEmailDay0(users)).
							Post("/invitations", handlers.Invitations.Create)
					} else {
						r.Post("/invitations", handlers.Invitations.Create)
					}
					r.Get("/invitations", handlers.Invitations.ListPending)
					r.Delete("/invitations/{inviteId}", handlers.Invitations.Revoke)
				}

				// PermAuditRead checked inside the handler (Owner+Admin via seed)
				if handlers.AuditLog != nil {
					r.Get("/audit-logs", handlers.AuditLog.List)
				}
			})
		})

		// public — outside the auth group so the marketing landing page can
		// render without a token.
		if handlers.Platforms != nil {
			r.Get("/platforms", handlers.Platforms.List)
		}
	})

	r.Handle("/metrics", promhttp.Handler())

	r.Get("/health/live", hc.LiveHandler())
	r.Get("/health/ready", hc.ReadyHandler())
	r.Get("/health", hc.LiveHandler()) // backward compat

	return r
}

// internalServiceIdentityAllowlist enumerates client cert CNs permitted to
// call identity-gated /internal/v1 routes. See docs/api/routes.md.
var internalServiceIdentityAllowlist = []string{"orchestrator", "api"}

// SetupInternal creates the internal mTLS-protected router.
func SetupInternal(handlers *Handlers, hc *health.Checker) *chi.Mux {
	r := chi.NewRouter()
	r.Use(chimiddleware.RequestID)
	r.Use(middleware.CorrelationID())
	r.Use(i18n.LocaleMiddleware)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)

	r.Get("/internal/v1/tokens", handlers.InternalToken.GetToken) // mTLS internal only

	// mTLS internal only — RequireServiceIdentity is defense-in-depth on top
	// of the listener-level handshake.
	if handlers.InternalBilling != nil {
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireServiceIdentity(internalServiceIdentityAllowlist, nil))
			r.Post("/internal/v1/billing/usage_logs", handlers.InternalBilling.LogUsage)
			r.Get("/internal/v1/billing/daily_spend", handlers.InternalBilling.GetDailySpend)
		})
	}

	r.Get("/health/live", hc.LiveHandler())
	r.Get("/health/ready", hc.ReadyHandler())
	r.Get("/health", hc.LiveHandler()) // backward compat

	return r
}
